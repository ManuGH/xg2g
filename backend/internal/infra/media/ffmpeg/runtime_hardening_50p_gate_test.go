// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func adaptiveHQ50Spec() ports.StreamSpec {
	return ports.StreamSpec{
		SessionID: "finalize-adaptive-50p-gate",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityHigh,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			PolicyModeHint: ports.RuntimeModeHQ25,
			TranscodeVideo: true,
			HWAccel:        "vaapi_encode_only",
			VideoCodec:     "av1",
			Deinterlace:    true,
			Container:      "fmp4",
			VideoMaxRateK:  6000,
			VideoBufSizeK:  12000,
			AudioBitrateK:  192,
		},
		Source: ports.StreamSource{ID: "1:0:19:EF75:3F9:1:C00000:0:0:0:", Type: ports.SourceTuner},
	}
}

func newAdaptive50pAdapter(t *testing.T, benchClass string) *LocalAdapter {
	t.Helper()
	a := NewLocalAdapter("ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard), "", "", 0, 0, false, 2*time.Second, 6, 0, 0, "")
	a.hostBenchmarkClassFn = func(string) string { return benchClass }
	return a
}

const adaptive50pURL = "http://127.0.0.1:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:"

// A "weak" 1080i50 host must NOT be promoted to HQ50 — the doubled framerate
// would overload it; it stays at the safe 25p. (Negative control: without the
// gate this would promote to HQ50 and the assert goes red.)
func TestFinalizePlan_AdaptiveHQ50_WeakHostStays25p(t *testing.T) {
	a := newAdaptive50pAdapter(t, "weak")
	finalized := a.FinalizePlan(context.Background(), adaptiveHQ50Spec(), adaptive50pURL)
	assert.NotEqual(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode,
		"weak host must not be promoted to 50p")
}

// A "moderate" host (~95ms 1080i50, like the staging box that plays 50p cleanly)
// IS promoted — the gate is "not weak", not the stricter "strong", so the
// favorite channel never regresses.
func TestFinalizePlan_AdaptiveHQ50_ModerateHostPromotes(t *testing.T) {
	a := newAdaptive50pAdapter(t, "moderate")
	finalized := a.FinalizePlan(context.Background(), adaptiveHQ50Spec(), adaptive50pURL)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode,
		"moderate host should still get 50p")
}

// Fail-open: an unmeasured host (empty class) still promotes — we only block
// hosts measured as weak, never regress un-benchmarked deployments.
func TestFinalizePlan_AdaptiveHQ50_UnmeasuredHostPromotes(t *testing.T) {
	a := newAdaptive50pAdapter(t, "")
	finalized := a.FinalizePlan(context.Background(), adaptiveHQ50Spec(), adaptive50pURL)
	assert.Equal(t, ports.RuntimeModeHQ50, finalized.Profile.EffectiveRuntimeMode,
		"unmeasured host should fail-open to 50p")
}
