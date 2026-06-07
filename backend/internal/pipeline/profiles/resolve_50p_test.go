// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package profiles

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func strongBenchmark() playbackprofile.HostBenchmarkSnapshot {
	return playbackprofile.HostBenchmarkSnapshot{Class: "strong"}
}

// A strong host promotes an interlaced transcode to 50p (HQ50).
func TestPromoteInterlacedTo50p_StrongPromotes(t *testing.T) {
	spec := model.ProfileSpec{TranscodeVideo: true, Deinterlace: true}
	promoteInterlacedTo50pIfCapable(&spec, strongBenchmark())
	if spec.EffectiveRuntimeMode != ports.RuntimeModeHQ50 {
		t.Fatalf("strong host: EffectiveRuntimeMode = %q, want hq50", spec.EffectiveRuntimeMode)
	}
}

// Negative control: a weak host stays at the safe default — a 50p transcode must
// never be forced onto a host that can't sustain it.
func TestPromoteInterlacedTo50p_WeakStays(t *testing.T) {
	spec := model.ProfileSpec{TranscodeVideo: true, Deinterlace: true}
	promoteInterlacedTo50pIfCapable(&spec, playbackprofile.HostBenchmarkSnapshot{Class: "weak"})
	if spec.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		t.Fatal("weak host must NOT be promoted to 50p")
	}
}

// Negative control: progressive (no deinterlace) is never promoted, even on a
// strong host.
func TestPromoteInterlacedTo50p_ProgressiveStays(t *testing.T) {
	spec := model.ProfileSpec{TranscodeVideo: true, Deinterlace: false}
	promoteInterlacedTo50pIfCapable(&spec, strongBenchmark())
	if spec.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		t.Fatal("progressive must never promote to 50p")
	}
}

// Negative control: copy/passthrough (no transcode) is never promoted.
func TestPromoteInterlacedTo50p_CopyStays(t *testing.T) {
	spec := model.ProfileSpec{TranscodeVideo: false, Deinterlace: true}
	promoteInterlacedTo50pIfCapable(&spec, strongBenchmark())
	if spec.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		t.Fatal("copy must never promote to 50p")
	}
}

// Backward compatibility: the plain 6-arg Resolve (used by every legacy caller
// and test) passes an empty benchmark, so it never promotes to 50p.
func TestResolveBackwardCompat_NoPromotionWithoutBenchmark(t *testing.T) {
	spec := Resolve("high", "", 0, nil, GPUBackendNone, HWAccelOff)
	if spec.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		t.Fatal("plain Resolve (no host context) must never produce hq50")
	}
}
