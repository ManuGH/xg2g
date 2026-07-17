package v3

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestNextPlaybackFeedbackPlan_NonSafariEscalatesToRepairFMP4(t *testing.T) {
	got := nextPlaybackFeedbackPlanWithResolver(model.ProfileSpec{
		Name:         profiles.ProfileAV1HW,
		Container:    "fmp4",
		DVRWindowSec: 2700,
		VideoCodec:   "av1",
	}, "", profiles.Resolver{})

	if got.id != playbackFeedbackFallbackPlanRepairFMP4 {
		t.Fatalf("nextPlaybackFeedbackPlan() id = %q, want %q", got.id, playbackFeedbackFallbackPlanRepairFMP4)
	}
	if got.reason != playbackFeedbackFallbackReasonDefaultRepair {
		t.Fatalf("nextPlaybackFeedbackPlan() reason = %q, want %q", got.reason, playbackFeedbackFallbackReasonDefaultRepair)
	}
	if got.profile.Name != profiles.ProfileRepair {
		t.Fatalf("nextPlaybackFeedbackPlan() name = %q, want %q", got.profile.Name, profiles.ProfileRepair)
	}
	if got.profile.Container != "fmp4" {
		t.Fatalf("nextPlaybackFeedbackPlan() container = %q, want %q", got.profile.Container, "fmp4")
	}
	if got.profile.VideoCodec != "libx264" {
		t.Fatalf("nextPlaybackFeedbackPlan() codec = %q, want %q", got.profile.VideoCodec, "libx264")
	}
	if got.profile.HWAccel != "" {
		t.Fatalf("nextPlaybackFeedbackPlan() hwaccel = %q, want empty", got.profile.HWAccel)
	}
	if got.profile.PolicyModeHint != ports.RuntimeModeSafe {
		t.Fatalf("nextPlaybackFeedbackPlan() mode = %q, want %q", got.profile.PolicyModeHint, ports.RuntimeModeSafe)
	}
	if got.profile.EffectiveModeSource != ports.RuntimeModeSourceFeedbackFallback {
		t.Fatalf("nextPlaybackFeedbackPlan() mode source = %q, want %q", got.profile.EffectiveModeSource, ports.RuntimeModeSourceFeedbackFallback)
	}
}

func TestNextSafariFeedbackPlan_AllowlistedFirstFailureUsesBrowserTSProfile(t *testing.T) {
	t.Setenv("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", "1:0:19:11:6:85:C00000:0:0:0:")

	got := nextSafariFeedbackPlanWithResolver(model.ProfileSpec{
		Name:         profiles.ProfileSafari,
		Container:    "fmp4",
		DVRWindowSec: 2700,
	}, "1:0:19:11:6:85:C00000:0:0:0:", profiles.LoadResolver())

	if got.id != playbackFeedbackFallbackPlanSafariBrowserTS {
		t.Fatalf("nextSafariFeedbackPlan() id = %q, want %q", got.id, playbackFeedbackFallbackPlanSafariBrowserTS)
	}
	if got.reason != playbackFeedbackFallbackReasonSafariForceCopyFirstFailure {
		t.Fatalf("nextSafariFeedbackPlan() reason = %q, want %q", got.reason, playbackFeedbackFallbackReasonSafariForceCopyFirstFailure)
	}
	if got.profile.Name != profiles.ProfileSafari {
		t.Fatalf("nextSafariFeedbackPlan() name = %q, want %q", got.profile.Name, profiles.ProfileSafari)
	}
	if got.profile.Container != "mpegts" {
		t.Fatalf("nextSafariFeedbackPlan() container = %q, want %q", got.profile.Container, "mpegts")
	}
	if !got.profile.DisableSafariForceCopy {
		t.Fatal("expected DisableSafariForceCopy to be enabled")
	}
	if got.profile.EffectiveModeSource != ports.RuntimeModeSourceFeedbackFallback {
		t.Fatalf("nextSafariFeedbackPlan() mode source = %q, want %q", got.profile.EffectiveModeSource, ports.RuntimeModeSourceFeedbackFallback)
	}
}

func TestNextSafariFeedbackPlan_AllowlistedRefailureUsesRepairTSProfile(t *testing.T) {
	t.Setenv("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", "1:0:19:11:6:85:C00000:0:0:0:")

	got := nextSafariFeedbackPlanWithResolver(model.ProfileSpec{
		Name:                   profiles.ProfileSafari,
		Container:              "mpegts",
		DVRWindowSec:           2700,
		DisableSafariForceCopy: true,
	}, "1:0:19:11:6:85:C00000:0:0:0:", profiles.LoadResolver())

	if got.id != playbackFeedbackFallbackPlanSafariRepairTS {
		t.Fatalf("nextSafariFeedbackPlan() id = %q, want %q", got.id, playbackFeedbackFallbackPlanSafariRepairTS)
	}
	if got.reason != playbackFeedbackFallbackReasonSafariForceCopyRepeatFailure {
		t.Fatalf("nextSafariFeedbackPlan() reason = %q, want %q", got.reason, playbackFeedbackFallbackReasonSafariForceCopyRepeatFailure)
	}
	if got.profile.Name != profiles.ProfileRepair {
		t.Fatalf("nextSafariFeedbackPlan() name = %q, want %q", got.profile.Name, profiles.ProfileRepair)
	}
	if got.profile.Container != "mpegts" {
		t.Fatalf("nextSafariFeedbackPlan() container = %q, want %q", got.profile.Container, "mpegts")
	}
	if got.profile.VideoCodec != "libx264" {
		t.Fatalf("nextSafariFeedbackPlan() codec = %q, want %q", got.profile.VideoCodec, "libx264")
	}
	if got.profile.PolicyModeHint != ports.RuntimeModeSafe {
		t.Fatalf("nextSafariFeedbackPlan() mode = %q, want %q", got.profile.PolicyModeHint, ports.RuntimeModeSafe)
	}
}
