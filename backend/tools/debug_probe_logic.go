package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/vod"
)

// Copied probeData struct
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
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

func main() {
	b, err := os.ReadFile("/root/xg2g/tools/monk_probe.json")
	if err != nil {
		panic(err)
	}

	var data probeData
	err = json.Unmarshal(b, &data)
	if err != nil {
		panic(err)
	}

	// Logic from probe.go
	info := &vod.StreamInfo{}

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

		case "audio":
			info.Audio.CodecName = s.CodecName
			info.Audio.TrackCount++
		}
	}

	if info.Video.Duration == 0 && data.Format.Duration != "" {
		if d, err := strconv.ParseFloat(data.Format.Duration, 64); err == nil {
			info.Video.Duration = d
		}
	}

	// Normalize
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
	info.Container = canonical

	fmt.Printf("Container: '%s'\n", info.Container)
	fmt.Printf("Video Codec: '%s'\n", info.Video.CodecName)
	fmt.Printf("Audio Codec: '%s'\n", info.Audio.CodecName)
	fmt.Printf("Audio Tracks: %d\n", info.Audio.TrackCount)
}
