package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// StreamProbeResult contains the simplified results of a stream analysis
type StreamProbeResult struct {
	Interlaced     bool
	Width          int
	Height         int
	CodecName      string
	AudioCodecName string
	RawJSON        string
}

// CanRemux returns true if the stream can be safely remuxed without transcoding.
// Criteria:
// - Video: h264 or hevc
// - Audio: aac or mp3 (AC3/DTS require transcode for Safari compatibility)
// - Not interlaced (requires deinterlacing)
func (r *StreamProbeResult) CanRemux(maxWidth int) bool {
	// Check video codec
	videoOK := r.CodecName == "h264" || r.CodecName == "hevc"
	if !videoOK {
		return false
	}

	// Check audio codec (if present)
	// AC3/DTS need transcoding to AAC for Safari
	if r.AudioCodecName != "" {
		audioOK := r.AudioCodecName == "aac" || r.AudioCodecName == "mp3"
		if !audioOK {
			return false
		}
	}

	// Check interlacing
	if r.Interlaced {
		return false
	}

	// Check resolution limit (if specified)
	if maxWidth > 0 && r.Width > maxWidth {
		return false
	}

	return true
}

// ProbeURL performs a deep analysis of the given URL using ffprobe
// It returns true if Interlacing is detected
func ProbeURL(ctx context.Context, url string) (*StreamProbeResult, error) {
	// 5 second timeout for usage in scanner
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// ffprobe arguments similar to recordings_remux.go but for network URL
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_entries", "stream=codec_type,codec_name,width,height,field_order",
		"-show_streams",
		// Analyze duration - relatively short to be fast, but long enough to find video stream
		"-analyzeduration", "5000000", // 5 seconds
		"-probesize", "5000000",
		"-i", url,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var data struct {
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			FieldOrder string `json:"field_order"` // progressive, tt, bb, tb, bt
		} `json:"streams"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		return nil, fmt.Errorf("json unmarshal failed: %w", err)
	}

	res := &StreamProbeResult{
		RawJSON: stdout.String(),
	}

	for _, s := range data.Streams {
		if s.CodecType == "video" {
			res.CodecName = s.CodecName
			res.Width = s.Width
			res.Height = s.Height

			// Detection logic:
			// "progressive" -> False
			// "tt", "bb", "tb", "bt" -> True
			// "unknown" or empty -> Assume False (Safe default for Copy, unless proven otherwise)
			// Pro 7 typically sends "tt" (Top Field First) or "mbaff" nuances that show up here.
			if s.FieldOrder != "progressive" && s.FieldOrder != "unknown" && s.FieldOrder != "" {
				res.Interlaced = true
			}
		}
		if s.CodecType == "audio" && res.AudioCodecName == "" {
			// Capture first audio codec
			res.AudioCodecName = s.CodecName
		}
	}

	return res, nil
}
