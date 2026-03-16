package profiles

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestResolveRequestedProfileWithOperatorOverride_ForceRepair(t *testing.T) {
	effective, snapshot := ResolveRequestedProfileWithOperatorOverride(ProfileCopy, config.PlaybackOperatorConfig{
		ForceIntent: "repair",
	})
	if effective != ProfileRepair {
		t.Fatalf("expected repair profile, got %q", effective)
	}
	if snapshot.ForcedIntent != playbackprofile.IntentRepair || !snapshot.OverrideApplied {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestResolveRequestedProfileWithSourceOperatorOverride_AppliesMatchingRuleOverlay(t *testing.T) {
	disableClientFallback := true
	effective, snapshot := ResolveRequestedProfileWithSourceOperatorOverride(ProfileCopy, OperatorRuleModeLive, "1:0:1:1337:42:99:0:0:0:0:", config.PlaybackOperatorConfig{
		MaxQualityRung: "compatible_audio_aac_256_stereo",
		SourceRules: []config.PlaybackOperatorRuleConfig{
			{
				Name:                  "problem-channel",
				Mode:                  OperatorRuleModeLive,
				ServiceRef:            "1:0:1:1337:42:99:0:0:0:0:",
				ForceIntent:           "repair",
				DisableClientFallback: &disableClientFallback,
			},
		},
	})
	if effective != ProfileRepair {
		t.Fatalf("expected repair profile, got %q", effective)
	}
	if snapshot.ForcedIntent != playbackprofile.IntentRepair {
		t.Fatalf("expected forced repair intent, got %q", snapshot.ForcedIntent)
	}
	if snapshot.MaxQualityRung != playbackprofile.RungCompatibleAudioAAC256Stereo {
		t.Fatalf("expected inherited compatible max quality rung, got %q", snapshot.MaxQualityRung)
	}
	if snapshot.RuleName != "problem-channel" || snapshot.RuleScope != OperatorRuleModeLive {
		t.Fatalf("expected matched rule metadata, got %#v", snapshot)
	}
	if !snapshot.DisableClientFallback || !snapshot.OverrideApplied {
		t.Fatalf("expected matched rule to disable client fallback and mark override applied, got %#v", snapshot)
	}
}

func TestResolveRequestedProfileWithSourceOperatorOverride_RuleCanClearGlobalClientFallbackDisable(t *testing.T) {
	disableClientFallback := false
	_, snapshot := ResolveRequestedProfileWithSourceOperatorOverride(ProfileHigh, OperatorRuleModeLive, "1:0:1:1337:42:99:0:0:0:0:", config.PlaybackOperatorConfig{
		DisableClientFallback: true,
		SourceRules: []config.PlaybackOperatorRuleConfig{
			{
				Name:                  "allow-restart",
				Mode:                  OperatorRuleModeLive,
				ServiceRefPrefix:      "1:0:1:1337:",
				DisableClientFallback: &disableClientFallback,
			},
		},
	})
	if snapshot.DisableClientFallback {
		t.Fatalf("expected matching rule to clear client fallback disable, got %#v", snapshot)
	}
	if !snapshot.OverrideApplied {
		t.Fatalf("expected matched source rule to mark override applied, got %#v", snapshot)
	}
}

func TestApplyMaxQualityRung_CapsAudioBitrate(t *testing.T) {
	spec, changed := ApplyMaxQualityRung(model.ProfileSpec{
		Name:          ProfileRepair,
		AudioBitrateK: 320,
	}, playbackprofile.RungRepairAudioAAC192Stereo)
	if !changed {
		t.Fatal("expected quality cap to change bitrate")
	}
	if spec.AudioBitrateK != 192 {
		t.Fatalf("expected capped bitrate 192, got %d", spec.AudioBitrateK)
	}
}

func TestApplyHostPressureOverride_DowngradesQualityClassProfiles(t *testing.T) {
	effective, changed := ApplyHostPressureOverride(ProfileSafariHEVCHW, playbackprofile.HostPressureConstrained)
	if !changed {
		t.Fatal("expected constrained host pressure to downgrade quality-class live profile")
	}
	if effective != ProfileHigh {
		t.Fatalf("expected host pressure downgrade to high, got %q", effective)
	}
}
