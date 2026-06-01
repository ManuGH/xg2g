// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
)

func TestAppendVaapiRateControlArgs_AV1QVBR(t *testing.T) {
	prof := ports.ProfileSpec{VideoMaxRateK: 8000, VideoBufSizeK: 16000}

	t.Run("default: AV1 emits QVBR with b:v + maxrate cap + quality target", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "av1")

		rc, ok := valueAfter(args, "-rc_mode")
		assert.True(t, ok)
		assert.Equal(t, "QVBR", rc)

		// QVBR requires -b:v; keep the 75% target headroom.
		bv, ok := valueAfter(args, "-b:v")
		assert.True(t, ok, "QVBR must carry -b:v (else 'Bitrate must be set for QVBR RC mode')")
		assert.Equal(t, "6000k", bv)

		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate, "maxrate stays the hard ceiling")

		gq, ok := valueAfter(args, "-global_quality")
		assert.True(t, ok)
		assert.Equal(t, "110", gq, "default quality target")

		assert.Contains(t, args, "-async_depth")
	})

	t.Run("XG2G_AV1_QVBR=false falls back to implicit VBR", func(t *testing.T) {
		t.Setenv("XG2G_AV1_QVBR", "false")
		args := appendVaapiRateControlArgs(nil, prof, "av1")

		assert.NotContains(t, args, "-rc_mode", "disabled QVBR must not set an explicit rc_mode")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "6000k", bv, "VBR fallback keeps the 75% target")
		maxrate, _ := valueAfter(args, "-maxrate")
		assert.Equal(t, "8000k", maxrate)
	})

	t.Run("XG2G_AV1_QVBR_QUALITY tunes the quality target", func(t *testing.T) {
		t.Setenv("XG2G_AV1_QVBR_QUALITY", "90")
		args := appendVaapiRateControlArgs(nil, prof, "av1")
		gq, _ := valueAfter(args, "-global_quality")
		assert.Equal(t, "90", gq)
	})

	t.Run("non-AV1 (h264) is unaffected — no QVBR, no quality target", func(t *testing.T) {
		args := appendVaapiRateControlArgs(nil, prof, "h264")
		assert.NotContains(t, args, "-rc_mode")
		assert.NotContains(t, args, "-global_quality")
		bv, _ := valueAfter(args, "-b:v")
		assert.Equal(t, "8000k", bv, "h264 keeps b:v == maxrate (no AV1 ring-stall workaround)")
	})

	t.Run("explicit VideoQP keeps the CQP branch (QVBR not applied)", func(t *testing.T) {
		cqp := ports.ProfileSpec{VideoQP: 110, VideoMaxRateK: 8000}
		args := appendVaapiRateControlArgs(nil, cqp, "av1")
		rc, _ := valueAfter(args, "-rc_mode")
		assert.Equal(t, "CQP", rc)
		assert.NotContains(t, args, "-b:v", "CQP branch does not emit -b:v")
	})
}
