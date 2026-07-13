package intents

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// TestKnownPlannerDivergence_NoTranscode explicitly tests that the Legacy Engine wrongfully allows the intent
// while the new Planner correctly denies it (video_codec_unsupported_for_copy), halting the cutover until this
// discrepancy is officially resolved.
func TestKnownPlannerDivergence_NoTranscode(t *testing.T) {
	evalTime := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	name := "8_Deny_DirectPlay_No_Transcode"
	sourceCap := scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "hevc", AudioCodec: "ac3", Width: 1920, Height: 1080, FPS: 50}
	clientFam := playbackprofile.ClientChromiumHLSJS

	// 1. Run genuine legacy path
	deps := newMockDeps()
	deps.scanner = &mockChannelScanner{found: true, capability: sourceCap}
	deps.hostPressure = playbackprofile.HostPressureAssessment{EffectiveBand: playbackprofile.HostPressureNormal}
	svc := NewService(deps)

	params := map[string]string{
		"allow_transcode": "0",
		"profile":         "compatible",
	}

	caps := capabilities.ResolveRuntimeProbeCapabilities(capabilities.PlaybackCapabilities{
		ClientFamilyFallback: clientFam,
		NetworkContext: &capabilities.NetworkContext{
			DownlinkKbps: 0,
		},
	})

	intent := Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-" + name,
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        params,
		CorrelationID: "corr-" + name,
		Mode:          model.ModeLive,
		UserAgent:     "unit-test",
		ClientCaps:    &caps,
		Logger:        zerolog.Nop(),
	}

	res, err := svc.ProcessIntent(context.Background(), intent)
	if err != nil {
		t.Fatalf("ProcessIntent failed: %v", err)
	}

	var legacyPlan playbackshadow.ComparablePlaybackPlan

	if res.Status == "accepted" {
		trace := deps.store.putSession.PlaybackTrace
		prof := deps.store.putSession.Profile
		legacyPlan = playbackshadow.ComparableFromLegacySession(trace, &prof)
	} else {
		// The intent was denied (e.g., stale truth, transcode restricted, etc.)
		legacyPlan = playbackshadow.ComparablePlaybackPlan{
			IsValid: true,
			Outcome: "deny",
		}
	}

	// Legacy actually allows this, which is wrong. Ensure we are still capturing this bug!
	assert.Equal(t, "allow", legacyPlan.Outcome, "Legacy is supposed to mistakenly allow this")

	// 2. Build PlaybackEvidence for the new planner using the exact same inputs
	legacyInput := playbackshadow.LegacyPlanningInput{
		Scope:              "live",
		RequestedIntent:    string(intent.Type),
		SourceIdentity:     "mock-source",
		ClientFamily:       clientFam,
		DownlinkKbps:       0,
		RTTMillis:          0,
		Container:          sourceCap.Container,
		VideoCodec:         sourceCap.VideoCodec,
		AudioCodec:         sourceCap.AudioCodec,
		Width:              sourceCap.Width,
		Height:             sourceCap.Height,
		FPS:                int(sourceCap.FPS),
		Interlaced:         sourceCap.Interlaced,
		HostPressureBand:   string(playbackprofile.HostPressureNormal),
		MaxQualityRung:     "",
		DisableTranscoding: true,
		MaxGlobalBitrate:   0,
		StrictFreshness:    false,
		Confidence:         "1.0",
	}

	// Extract supported capabilities as the legacy playbackshadow builder does:
	legacyInput.SupportedContainers = caps.Containers
	legacyInput.SupportedVideoCodecs = caps.VideoCodecs
	legacyInput.SupportedAudioCodecs = caps.AudioCodecs
	legacyInput.SupportedEngines = caps.HLSEngines
	legacyInput.PreferredEngine = caps.PreferredHLSEngine
	legacyInput.SupportsHls = caps.SupportsHLS
	legacyInput.SupportsRange = caps.SupportsRange
	legacyInput.DeviceType = caps.DeviceType
	if caps.AllowTranscode != nil {
		legacyInput.AllowTranscode = *caps.AllowTranscode
	} else {
		legacyInput.AllowTranscode = true
	}
	if caps.MaxVideo != nil {
		legacyInput.MaxVideoWidth = caps.MaxVideo.Width
		legacyInput.MaxVideoHeight = caps.MaxVideo.Height
		legacyInput.MaxVideoFPS = caps.MaxVideo.Fps
	}

	ev, buildErr := playbackshadow.BuildPlaybackEvidence(legacyInput)
	if buildErr != nil {
		t.Fatalf("BuildPlaybackEvidence failed: %v", buildErr)
	}

	// Override the timestamp so it's stable
	ev.ObservedAt = evalTime.UnixMilli()
	ev.NetworkCaptureTime = evalTime.UnixMilli()

	// 3. Execute new Planner
	planRes, planErr := playbackplanner.Plan(ev)
	if planErr != nil {
		t.Fatalf("Plan() returned error: %v", planErr)
	}

	// Ensure Planner smartly denies it with the proper reason code
	assert.Equal(t, playbackplanner.DecisionDeny, planRes.Plan.Decision)
	assert.Equal(t, playbackplanner.ReasonPolicyDeniesTranscode, planRes.Plan.ReasonCode)

	newComp := playbackshadow.ComparableFromPlanner(planRes.Plan)
	assert.Equal(t, "deny", newComp.Outcome)

	// Compare just to verify the exact diff codes are reported
	diffs := playbackshadow.DiffComparablePlans(legacyPlan, newComp)
	assert.ElementsMatch(t, []string{"outcome_mismatch", "mode_mismatch", "engine_mismatch", "packaging_mismatch", "video_mode_mismatch", "audio_mode_mismatch", "video_codec_mismatch", "audio_codec_mismatch"}, diffs)
}
