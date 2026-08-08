package manager

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// TestStartupRecovery_EncoderCrashHardensRuntimeMode covers the six ffmpeg faults
// observed on this deployment — all VAAPI, four of them the same null dereference
// in libavcodec. Every one ended its session outright, because a crash produced
// the detail "process exit code -1", which matched no recovery rule. A fault on
// the encode path is precisely what a lighter runtime profile exists for.
func TestStartupRecovery_EncoderCrashHardensRuntimeMode(t *testing.T) {
	hq50 := model.ProfileSpec{
		Name:                 "av1_hw",
		TranscodeVideo:       true,
		HWAccel:              "vaapi",
		EffectiveRuntimeMode: ports.RuntimeModeHQ50,
	}

	next, promote := startupRecoveryProfileWithResolver(
		hq50, model.RProcessEnded, "encoder process crashed (segmentation fault)", nil)
	if !promote {
		t.Fatal("an encoder crash on HQ50 must harden the runtime mode instead of ending the session")
	}
	if next.EffectiveRuntimeMode != ports.RuntimeModeHQ25 || !next.ForceSafariHQ25 {
		t.Fatalf("expected a fallback to HQ25, got mode=%v forced=%v", next.EffectiveRuntimeMode, next.ForceSafariHQ25)
	}

	// One step further down: HQ25 crashing falls through to the repair profile.
	hq25 := next
	resolver := func(profileID string, dvrWindowSec int) model.ProfileSpec {
		return model.ProfileSpec{Name: profileID, DVRWindowSec: dvrWindowSec}
	}
	next, promote = startupRecoveryProfileWithResolver(
		hq25, model.RProcessEnded, "encoder process crashed (aborted)", resolver)
	if !promote || next.Name != profiles.ProfileRepair {
		t.Fatalf("an encoder crash on HQ25 must fall back to %q, got %q (promote=%v)", profiles.ProfileRepair, next.Name, promote)
	}
}

// TestStartupRecovery_InputFailuresDoNotHardenProfile is the guard on the other
// side: no profile change repairs an unusable upstream, so those failures must not
// buy a restart. Session 1619cee0 spent 52s proving that on a scrambled service.
func TestStartupRecovery_InputFailuresDoNotHardenProfile(t *testing.T) {
	hq50 := model.ProfileSpec{
		Name:                 "av1_hw",
		EffectiveRuntimeMode: ports.RuntimeModeHQ50,
	}
	for _, detail := range []string{
		"preflight failed scrambled",
		"upstream stream ended prematurely",
		"failed to open upstream input",
		"invalid upstream input data",
	} {
		if _, promote := startupRecoveryProfileWithResolver(hq50, model.RProcessEnded, detail, nil); promote {
			t.Fatalf("input failure %q must not trigger a profile fallback", detail)
		}
	}
}

func TestIsStartupHardeningDetail(t *testing.T) {
	for _, detail := range []string{
		"transcode stalled - no progress detected",
		"process died during startup: transcode stalled - no progress detected",
		"encoder process crashed (segmentation fault)",
		"encoder process crashed (bus error)",
	} {
		if !isStartupHardeningDetail(detail) {
			t.Fatalf("%q should be recoverable by hardening the profile", detail)
		}
	}
	for _, detail := range []string{
		"process exit code 1",
		"process terminated by signal (killed)",
		"preflight failed scrambled",
		"",
	} {
		if isStartupHardeningDetail(detail) {
			t.Fatalf("%q must not trigger a profile fallback", detail)
		}
	}
}
