package ffmpeg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/rs/zerolog/log"
)

// Prober implements vod.Prober interface using ffprobe.
type Prober struct {
	BinaryPath string
}

type ProbeOptions struct {
	AnalyzeDuration time.Duration
	ProbeSizeBytes  int64
}

var ffprobeURLPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^'"\s]+`)

func NewProber(binaryPath string) *Prober {
	return &Prober{BinaryPath: strings.TrimSpace(binaryPath)}
}

func (p *Prober) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return probeWithBinAndOptions(ctx, p.BinaryPath, path, ProbeOptions{})
}

// Probe executes ffprobe and returns stream info.
func Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return probeWithBinAndOptions(ctx, "", path, ProbeOptions{})
}

// ProbeWithOptions executes ffprobe and returns stream info with optional
// analyze/probe budget overrides.
func ProbeWithOptions(ctx context.Context, path string, opts ProbeOptions) (*vod.StreamInfo, error) {
	return probeWithBinAndOptions(ctx, "", path, opts)
}

// ProbeWithBin executes ffprobe and returns stream info.
// If binaryPath is empty, it falls back to PATH resolution ("ffprobe").
func ProbeWithBin(ctx context.Context, binaryPath string, path string) (*vod.StreamInfo, error) {
	return probeWithBinAndOptions(ctx, binaryPath, path, ProbeOptions{})
}

func probeWithBinAndOptions(ctx context.Context, binaryPath string, path string, opts ProbeOptions) (*vod.StreamInfo, error) {
	headers := "Connection: close\r\n"
	if u, err := url.Parse(path); err == nil && u.User != nil {
		pwd, _ := u.User.Password()
		auth := u.User.Username() + ":" + pwd
		headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
		// Strip userinfo from the probe URL now that credentials are in the
		// Authorization header, so they cannot leak via /proc/<pid>/cmdline
		// of the ffprobe subprocess.
		u.User = nil
		path = u.String()
	}

	args := buildProbeArgs(path, headers, opts)

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
			errStr := sanitizeProbeText(stderr.String())
			if len(errStr) > 4096 {
				errStr = errStr[:4096] + "..."
			}
			log.Warn().Err(err).Str("path", sanitizeProbePathForLog(path)).Str("stderr", errStr).Msg("ffprobe non-zero exit but JSON accepted")
		}
	} else if err != nil {
		// Execution failed AND/OR no usable JSON.
		errStr := sanitizeProbeText(stderr.String())
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
			info.Video.FieldOrder = strings.ToLower(strings.TrimSpace(s.FieldOrder))
			if info.Video.FieldOrder != "" && info.Video.FieldOrder != "progressive" {
				info.Video.Interlaced = true
			}
			if fps := parseFrameRate(s.AvgFrameRate); fps > 0 {
				info.Video.FPS = fps
			}
			if signalFPS := parseFrameRate(s.RFrameRate); signalFPS > 0 {
				info.Video.SignalFPS = signalFPS
			}
			if info.Video.SignalFPS == 0 {
				info.Video.SignalFPS = info.Video.FPS
			}
			if info.BitrateKbps == 0 && s.BitRate != "" {
				if bitrateKbps := parseBitrateKbps(s.BitRate); bitrateKbps > 0 {
					info.BitrateKbps = bitrateKbps
				}
			}

		case "audio":
			info.Audio.CodecName = s.CodecName
			if s.SampleRate != "" {
				if sampleRate, err := strconv.Atoi(s.SampleRate); err == nil {
					info.Audio.SampleRate = sampleRate
				}
			}
			if s.Channels > 0 {
				info.Audio.Channels = s.Channels
			}
			if bitrateKbps := parseBitrateKbps(s.BitRate); bitrateKbps > 0 {
				info.Audio.BitrateKbps = bitrateKbps
			}
			info.Audio.ChannelLayout = strings.ToLower(strings.TrimSpace(s.ChannelLayout))
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
	if bitrateKbps := parseBitrateKbps(data.Format.BitRate); bitrateKbps > 0 {
		info.BitrateKbps = bitrateKbps
	}

	return info, nil
}

func parseBitrateKbps(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	bitrate, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || bitrate <= 0 {
		return 0
	}
	return int((bitrate + 999) / 1000)
}

func parseFrameRate(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0/0" {
		return 0
	}
	parts := strings.Split(raw, "/")
	if len(parts) == 2 {
		num, errNum := strconv.ParseFloat(parts[0], 64)
		den, errDen := strconv.ParseFloat(parts[1], 64)
		if errNum == nil && errDen == nil && den > 0 {
			return num / den
		}
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func sanitizeProbePathForLog(path string) string {
	if strings.Contains(path, "://") {
		return platformnet.SanitizeURL(path)
	}
	return path
}

func sanitizeProbeText(raw string) string {
	return ffprobeURLPattern.ReplaceAllStringFunc(raw, platformnet.SanitizeURL)
}

type probeData struct {
	Streams []struct {
		CodecType        string `json:"codec_type"`
		CodecName        string `json:"codec_name"`
		PixFmt           string `json:"pix_fmt,omitempty"`
		BitsPerRawSample string `json:"bits_per_raw_sample,omitempty"`
		BitRate          string `json:"bit_rate,omitempty"`
		Duration         string `json:"duration,omitempty"`
		SampleRate       string `json:"sample_rate,omitempty"`
		Channels         int    `json:"channels,omitempty"`
		ChannelLayout    string `json:"channel_layout,omitempty"`
		Width            int    `json:"width,omitempty"`
		Height           int    `json:"height,omitempty"`
		FieldOrder       string `json:"field_order,omitempty"`
		AvgFrameRate     string `json:"avg_frame_rate,omitempty"`
		RFrameRate       string `json:"r_frame_rate,omitempty"`
	} `json:"streams"`
	Format struct {
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate,omitempty"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

// buildProbeArgs assembles the ffprobe argument list for a probe of path.
//
// For HTTP(S) inputs it mirrors the live-playback reconnect tolerance (see
// media/ffmpeg/plan_input.go). A capability probe of a live channel races the
// receiver's cold tune + descramble (FBC lock / ECM); during that window the
// stream relay returns a premature EOF / "Input/output error" at byte 0. Without
// reconnect, ffprobe gives up and the channel is flagged failed/cold for 24h —
// exactly the pay-TV channels that most need pre-warming. With the reconnect
// options ffprobe re-opens through that cold window (bounded by the caller's
// context timeout and -reconnect_delay_max) and probes once real data flows, the
// same way live ffmpeg already succeeds on these channels. Non-HTTP inputs (local
// files) get no reconnect flags, since they are HTTP-protocol options.
func buildProbeArgs(path, headers string, opts ProbeOptions) []string {
	args := []string{
		"-v", "error",
		"-user_agent", "VLC/3.0.21 LibVLC/3.0.21",
		"-headers", headers,
	}
	if whitelist, ok := InputProtocolWhitelist(path); ok {
		args = append(args, "-protocol_whitelist", whitelist)
	}
	if isHTTPProbeInput(path) {
		args = append(args,
			"-reconnect", "1",
			"-reconnect_at_eof", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "2",
			"-reconnect_on_network_error", "1",
			"-reconnect_on_http_error", "4xx,5xx",
		)
	}
	if opts.AnalyzeDuration > 0 {
		args = append(args, "-analyzeduration", strconv.FormatInt(opts.AnalyzeDuration.Microseconds(), 10))
	}
	if opts.ProbeSizeBytes > 0 {
		args = append(args, "-probesize", strconv.FormatInt(opts.ProbeSizeBytes, 10))
	}
	args = append(args,
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	return args
}

// isHTTPProbeInput reports whether path is an http(s) URL, for which the reconnect
// options are meaningful (they are HTTP-protocol options in libavformat).
func isHTTPProbeInput(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}
