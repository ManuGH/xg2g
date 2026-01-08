package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/ManuGH/xg2g/internal/control/vod"
)

// Prober implements vod.Prober interface using ffprobe.
type Prober struct{}

func NewProber() *Prober {
	return &Prober{}
}

func (p *Prober) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return Probe(ctx, path)
}

// Probe executes ffprobe and returns stream info.
func Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var data probeData
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}

	info := &vod.StreamInfo{}

	// Parse streams
	for _, s := range data.Streams {
		if s.CodecType == "video" {
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

		} else if s.CodecType == "audio" {
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
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}
