// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package raster verifies presentation timestamp (PTS) invariants on HLS streams.
//
// Broadcast DVB/ATSC H.264 streams frequently use open GOPs. When muxed into
// HLS segments (particularly fMP4), encoder and muxer boundary clamping can
// introduce duplicate timestamps or single-frame holes across segment cuts.
// Players render these discontinuities as periodic visible judder ("es flackert").
//
// This package provides machine-verifiable checks for presentation rasters so that
// both CI pipelines and runtime stream sampling can treat ffmpeg output as
// untrusted input and assert strict monotonicity on a constant frame grid.
package raster

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Report summarizes the result of checking presentation timestamps against an
// expected frame interval raster.
type Report struct {
	// TotalFrames is the number of video packets evaluated.
	TotalFrames int `json:"total_frames"`
	// DuplicatePTS is the count of consecutive frames whose PTS delta is near zero (< tolerance).
	DuplicatePTS int `json:"duplicate_pts"`
	// Holes is the count of frame transitions where the PTS delta exceeds one frame interval (+ tolerance).
	Holes int `json:"holes"`
	// NonMonotonic is the count of out-of-order presentation timestamps before sorting.
	NonMonotonic int `json:"non_monotonic"`
	// MinDelta is the smallest observed PTS delta between consecutive frames.
	MinDelta float64 `json:"min_delta"`
	// MaxDelta is the largest observed PTS delta between consecutive frames.
	MaxDelta float64 `json:"max_delta"`
	// Valid is true iff the presentation timeline has no duplicates and no missing frame intervals (holes).
	Valid bool `json:"valid"`
}

// ValidatePTS checks whether the given sequence of presentation timestamps
// adheres strictly to expectedIntervalSec within toleranceSec on the presentation timeline.
func ValidatePTS(pts []float64, expectedIntervalSec, toleranceSec float64) Report {
	rep := Report{
		TotalFrames: len(pts),
		MinDelta:    math.MaxFloat64,
	}
	if len(pts) < 2 {
		rep.MinDelta = 0
		rep.Valid = true
		return rep
	}

	// Check monotonicity on raw container sequence (records B-frame DTS/PTS divergence)
	for i := 1; i < len(pts); i++ {
		if pts[i] < pts[i-1]-toleranceSec {
			rep.NonMonotonic++
		}
	}

	sorted := append([]float64(nil), pts...)
	sort.Float64s(sorted)

	for i := 1; i < len(sorted); i++ {
		delta := sorted[i] - sorted[i-1]
		if delta < rep.MinDelta {
			rep.MinDelta = delta
		}
		if delta > rep.MaxDelta {
			rep.MaxDelta = delta
		}

		if delta < toleranceSec {
			rep.DuplicatePTS++
		} else if delta > expectedIntervalSec+toleranceSec {
			rep.Holes++
		}
	}

	rep.Valid = rep.DuplicatePTS == 0 && rep.Holes == 0
	return rep
}

// Runner abstracts command execution for probing streams.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands using os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// ProbeVideoPTS invokes ffprobe to extract video packet presentation timestamps
// from a media file or HLS playlist URL/path.
func ProbeVideoPTS(ctx context.Context, runner Runner, targetPath string) ([]float64, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	out, err := runner.Run(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v",
		"-show_entries", "packet=pts_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		targetPath,
	)
	if err != nil {
		return nil, fmt.Errorf("ffprobe pts_time extraction failed: %w", err)
	}

	var pts []float64
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimRight(strings.TrimSpace(scanner.Text()), ",")
		if line == "" || line == "N/A" {
			continue
		}
		val, parseErr := strconv.ParseFloat(line, 64)
		if parseErr != nil {
			continue
		}
		pts = append(pts, val)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading ffprobe output: %w", err)
	}
	return pts, nil
}

// ValidateMedia invokes ffprobe against targetPath and verifies that its video
// presentation timestamps match expectedFPS cleanly.
func ValidateMedia(ctx context.Context, runner Runner, targetPath string, expectedFPS float64) (*Report, error) {
	if expectedFPS <= 0 {
		return nil, fmt.Errorf("expectedFPS must be > 0, got %f", expectedFPS)
	}
	pts, err := ProbeVideoPTS(ctx, runner, targetPath)
	if err != nil {
		return nil, err
	}
	interval := 1.0 / expectedFPS
	// Tolerance: 2ms is well below half a frame at 60fps (~8.33ms), yet accommodates float rounding.
	rep := ValidatePTS(pts, interval, 0.002)
	return &rep, nil
}
