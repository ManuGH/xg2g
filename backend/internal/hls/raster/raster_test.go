// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package raster

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePTS_Clean50FPS(t *testing.T) {
	var pts []float64
	for i := 0; i < 100; i++ {
		pts = append(pts, float64(i)*0.02) // 50fps = 20ms interval
	}
	rep := ValidatePTS(pts, 0.02, 0.002)
	assert.True(t, rep.Valid)
	assert.Equal(t, 100, rep.TotalFrames)
	assert.Equal(t, 0, rep.DuplicatePTS)
	assert.Equal(t, 0, rep.Holes)
	assert.Equal(t, 0, rep.NonMonotonic)
	assert.InDelta(t, 0.02, rep.MinDelta, 0.0001)
	assert.InDelta(t, 0.02, rep.MaxDelta, 0.0001)
}

func TestValidatePTS_DuplicateDetected(t *testing.T) {
	// Frame 1 and Frame 2 have identical timestamp (duplicate frame bug)
	pts := []float64{0.00, 0.02, 0.02, 0.04, 0.06}
	rep := ValidatePTS(pts, 0.02, 0.002)
	assert.False(t, rep.Valid)
	assert.Equal(t, 1, rep.DuplicatePTS)
	assert.Equal(t, 0, rep.Holes)
}

func TestValidatePTS_HoleDetected(t *testing.T) {
	// Frame skipped between 0.02 and 0.06 (hole bug at segment cut)
	pts := []float64{0.00, 0.02, 0.06, 0.08}
	rep := ValidatePTS(pts, 0.02, 0.002)
	assert.False(t, rep.Valid)
	assert.Equal(t, 0, rep.DuplicatePTS)
	assert.Equal(t, 1, rep.Holes)
}

func TestValidatePTS_NonMonotonicDetected(t *testing.T) {
	// Timestamps out of presentation order in raw container packet order
	pts := []float64{0.00, 0.04, 0.02, 0.06}
	rep := ValidatePTS(pts, 0.02, 0.002)
	assert.True(t, rep.Valid)
	assert.Equal(t, 1, rep.NonMonotonic)
	assert.Equal(t, 0, rep.DuplicatePTS)
	assert.Equal(t, 0, rep.Holes)
}

func TestValidatePTS_SmallSequence(t *testing.T) {
	rep := ValidatePTS([]float64{0.00}, 0.02, 0.002)
	assert.True(t, rep.Valid)
	assert.Equal(t, 1, rep.TotalFrames)

	rep0 := ValidatePTS(nil, 0.02, 0.002)
	assert.True(t, rep0.Valid)
	assert.Equal(t, 0, rep0.TotalFrames)
}

type mockRunner struct {
	output []byte
	err    error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return m.output, m.err
}

func TestProbeVideoPTS(t *testing.T) {
	output := []byte(`0.020000
0.040000,
N/A
0.060000

`)
	runner := &mockRunner{output: output}
	pts, err := ProbeVideoPTS(context.Background(), runner, "test.m3u8")
	require.NoError(t, err)
	assert.Equal(t, []float64{0.02, 0.04, 0.06}, pts)
}

func TestValidateMedia_ErrorHandling(t *testing.T) {
	runner := &mockRunner{err: errors.New("exec fail")}
	_, err := ValidateMedia(context.Background(), runner, "bad.m3u8", 50.0)
	assert.ErrorContains(t, err, "pts_time extraction failed")

	_, err = ValidateMedia(context.Background(), runner, "bad.m3u8", -10.0)
	assert.ErrorContains(t, err, "expectedFPS must be > 0")
}
