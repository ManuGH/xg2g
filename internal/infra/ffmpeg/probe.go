package ffmpeg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/rs/zerolog/log"
)

// Prober implements vod.Prober interface using ffprobe.
type Prober struct {
	BinaryPath string
}

func NewProber(binaryPath string) *Prober {
	return &Prober{BinaryPath: strings.TrimSpace(binaryPath)}
}

func (p *Prober) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return ProbeWithBin(ctx, p.BinaryPath, path)
}

// Probe executes ffprobe and returns stream info.
func Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return ProbeWithBin(ctx, "", path)
}

// ProbeWithBin executes ffprobe and returns stream info.
// If binaryPath is empty, it falls back to PATH resolution ("ffprobe").
func ProbeWithBin(ctx context.Context, binaryPath string, path string) (*vod.StreamInfo, error) {
	headers := "Connection: close\r\n"
	if u, err := url.Parse(path); err == nil && u.User != nil {
		pwd, _ := u.User.Password()
		auth := u.User.Username() + ":" + pwd
		headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
	}

	args := []string{
		"-v", "error",
		"-user_agent", "VLC/3.0.21 LibVLC/3.0.21",
		"-headers", headers,
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}

	ffprobeBin := strings.TrimSpace(binaryPath)
	if ffprobeBin == "" {
		ffprobeBin = "ffprobe" // PATH fallback (last resort)
	}

	// #nosec G204 - ffprobe path is configured; args are strictly controlled and path is opaque
	cmd := exec.CommandContext(ctx, ffprobeBin, args...)

	// Capture stderr for diagnostics (because exit code might be non-zero even with valid JSON)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()

	var data probeData
	jsonErr := json.Unmarshal(out, &data)

	// Validate: Must be valid JSON AND have actual playable content (Video or Audio with codec)
	// Audio-only recordings are considered playable; video truth is not required for probe success.
	hasPlayableStream := false
	if jsonErr == nil {
		for _, s := range data.Streams {
			// Require CodecName to be present to treat as valid stream
			if (s.CodecType == "video" || s.CodecType == "audio") && s.CodecName != "" {
				hasPlayableStream = true
				break
			}
		}
	}

	isValid := jsonErr == nil && data.Format.FormatName != "" && hasPlayableStream

	if isValid {
		// Treat valid JSON with playable content as success even if ffprobe exits non-zero.
		if err != nil {
			// Log warning about non-zero exit (likely partial file or warnings).
			// Truncate stderr to prevent log explosion on massive dumps.
			errStr := stderr.String()
			if len(errStr) > 4096 {
				errStr = errStr[:4096] + "..."
			}
			log.Warn().Err(err).Str("path", path).Str("stderr", errStr).Msg("ffprobe non-zero exit but JSON accepted")
		}
	} else if err != nil {
		// Execution failed AND/OR no usable JSON.
		errStr := stderr.String()
		if len(errStr) > 4096 {
			errStr = errStr[:4096] + "..."
		}
		return nil, fmt.Errorf("ffprobe failed: %w (stderr: %s)", err, errStr)
	} else if jsonErr != nil {
		return nil, fmt.Errorf("json decode: %w", jsonErr)
	} else {
		return nil, fmt.Errorf("ffprobe returned empty data (no playable streams)")
	}

	info := &vod.StreamInfo{}

	// Parse streams
	for _, s := range data.Streams {
		switch s.CodecType {
		case "video":
			info.Video.CodecName = s.CodecName
			info.Video.PixFmt = s.PixFmt
			if s.BitsPerRawSample != "" {
				if v, err := strconv.Atoi(s.BitsPerRawSample); err == nil {
					info.Video.BitDepth = v
				}
			}
			// Fallback bit depth from pix_fmt if needed...
			if info.Video.BitDepth == 0 && s.PixFmt == "yuv420p10le" {
				info.Video.BitDepth = 10
			} else if info.Video.BitDepth == 0 {
				info.Video.BitDepth = 8
			}

			if s.Duration != "" {
				if d, err := strconv.ParseFloat(s.Duration, 64); err == nil {
					info.Video.Duration = d
				}
			}
			info.Video.Width = s.Width
			info.Video.Height = s.Height
			if s.FieldOrder != "" && s.FieldOrder != "progressive" {
				info.Video.Interlaced = true
			}
			if s.AvgFrameRate != "" && s.AvgFrameRate != "0/0" {
				parts := strings.Split(s.AvgFrameRate, "/")
				if len(parts) == 2 {
					num, _ := strconv.ParseFloat(parts[0], 64)
					den, _ := strconv.ParseFloat(parts[1], 64)
					if den > 0 {
						info.Video.FPS = num / den
					}
				}
			}

		case "audio":
			info.Audio.CodecName = s.CodecName
			info.Audio.TrackCount++
		}
	}

	// Format level duration check if stream duration missing?
	if info.Video.Duration == 0 && data.Format.Duration != "" {
		if d, err := strconv.ParseFloat(data.Format.Duration, 64); err == nil {
			info.Video.Duration = d
		}
	}

	// Normalize container vocab (handle comma-lists and prefer mpegts -> ts)
	parts := strings.Split(data.Format.FormatName, ",")
	canonical := ""
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "mpegts" {
			canonical = "ts"
			break
		}
		if canonical == "" && t != "" {
			canonical = t
		}
	}

	if canonical == "" {
		return nil, fmt.Errorf("ffprobe returned empty format_name token list")
	}
	info.Container = canonical

	return info, nil
}

type probeData struct {
	Streams []struct {
		CodecType        string `json:"codec_type"`
		CodecName        string `json:"codec_name"`
		PixFmt           string `json:"pix_fmt,omitempty"`
		BitsPerRawSample string `json:"bits_per_raw_sample,omitempty"`
		Duration         string `json:"duration,omitempty"`
		Width            int    `json:"width,omitempty"`
		Height           int    `json:"height,omitempty"`
		FieldOrder       string `json:"field_order,omitempty"`
		AvgFrameRate     string `json:"avg_frame_rate,omitempty"`
	} `json:"streams"`
	Format struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}
