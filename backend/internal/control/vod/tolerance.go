package vod

import (
	"fmt"
	"math"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/log"
)

// ValidateSourceTruth implements the tolerance contract comparing the expected source truth
// from the planner against the actual probed stream info.
//
// Tolerance rules:
// - Video codec: HARD (exact match)
// - Audio codec: HARD (exact match)
// - Container: HARD (exact match)
// - Bit depth: HARD (exact match)
// - Resolution: HARD on class boundary, SOFT within class
// - Duration: SOFT (±2.0s or ±0.5%)
// - Bitrate, FPS: SOFT
//
// Returns a TruthMismatch if a HARD violation occurs, nil otherwise.
func ValidateSourceTruth(truth ports.SourceProfile, probed StreamInfo) *ports.TruthMismatch {
	// 1. HARD: Video Codec
	if truth.VideoCodec != "" && probed.Video.CodecName != "" && truth.VideoCodec != probed.Video.CodecName {
		return &ports.TruthMismatch{
			Field:    "videoCodec",
			Expected: truth.VideoCodec,
			Actual:   probed.Video.CodecName,
		}
	}

	// 2. HARD: Audio Codec
	if truth.AudioCodec != "" && probed.Audio.CodecName != "" && truth.AudioCodec != probed.Audio.CodecName {
		return &ports.TruthMismatch{
			Field:    "audioCodec",
			Expected: truth.AudioCodec,
			Actual:   probed.Audio.CodecName,
		}
	}

	// 3. HARD: Container
	if truth.Container != "" && probed.Container != "" {
		match := false
		for _, format := range strings.Split(probed.Container, ",") {
			if strings.TrimSpace(format) == truth.Container {
				match = true
				break
			}
		}
		if !match {
			return &ports.TruthMismatch{
				Field:    "container",
				Expected: truth.Container,
				Actual:   probed.Container,
			}
		}
	}

	// 4. HARD: Bit depth
	if truth.BitDepth > 0 && probed.Video.BitDepth > 0 && truth.BitDepth != probed.Video.BitDepth {
		return &ports.TruthMismatch{
			Field:    "bitDepth",
			Expected: fmt.Sprintf("%d", truth.BitDepth),
			Actual:   fmt.Sprintf("%d", probed.Video.BitDepth),
		}
	}

	// 5. HARD: Resolution class boundaries
	// SD < 720p <= HD < 1080p <= FHD < 2160p <= UHD
	if truth.Height > 0 && probed.Video.Height > 0 {
		truthClass := resolutionClass(truth.Height)
		probedClass := resolutionClass(probed.Video.Height)
		if truthClass != probedClass {
			return &ports.TruthMismatch{
				Field:    "resolution",
				Expected: truthClass,
				Actual:   probedClass,
			}
		} else if truth.Height != probed.Video.Height {
			log.L().Info().
				Int("expected", truth.Height).
				Int("actual", probed.Video.Height).
				Msg("SOFT deviation: resolution differs within class")
		}
	}

	// 6. SOFT: Duration
	if truth.Duration > 0 && probed.Video.Duration > 0 {
		diff := math.Abs(truth.Duration - probed.Video.Duration)
		pct := diff / truth.Duration
		if diff > 2.0 && pct > 0.005 {
			log.L().Info().
				Float64("expected", truth.Duration).
				Float64("actual", probed.Video.Duration).
				Msg("SOFT deviation: duration exceeds tolerance (±2.0s or ±0.5%)")
		}
	}

	// 7. SOFT: Bitrate & FPS
	if truth.BitrateKbps > 0 && probed.BitrateKbps > 0 && truth.BitrateKbps != probed.BitrateKbps {
		log.L().Info().
			Int("expected", truth.BitrateKbps).
			Int("actual", probed.BitrateKbps).
			Msg("SOFT deviation: bitrate differs")
	}
	if truth.FPS > 0 && probed.Video.FPS > 0 && math.Abs(truth.FPS-probed.Video.FPS) > 0.1 {
		log.L().Info().
			Float64("expected", truth.FPS).
			Float64("actual", probed.Video.FPS).
			Msg("SOFT deviation: FPS differs")
	}

	return nil
}

func resolutionClass(height int) string {
	if height < 720 {
		return "SD"
	}
	if height < 1080 {
		return "HD"
	}
	if height < 2160 {
		return "FHD"
	}
	return "UHD"
}
