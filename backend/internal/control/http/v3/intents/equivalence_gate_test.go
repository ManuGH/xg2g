package intents

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/intents/testfixtures"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/rs/zerolog"
)

const dirtyDVBLegacyIntentFixture = "6_Dirty_DVB_Fallback"

// TestLegacyIntentAdapterDifferential characterizes only zero-diff cases in the
// pre-receipt ProcessIntent adapter. Known adapter-only divergences live in
// divergence_test.go. The actual cutover gate remains the /stream-info boundary
// in recordings/playback_info_shadow_test.go.
func TestLegacyIntentAdapterDifferential(t *testing.T) {
	evalTime := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)

	for _, tc := range testfixtures.Cases {
		if tc.Name == dirtyDVBLegacyIntentFixture {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			legacyPlan, plannerPlan, diffs := runLegacyIntentAdapterDifferential(t, tc, evalTime)
			if len(diffs) > 0 {
				t.Fatalf("unexpected legacy-adapter diffs: %v\n\tLegacy: %+v\n\tPlanner: %+v", diffs, legacyPlan, plannerPlan)
			}
		})
	}
}

func runLegacyIntentAdapterDifferential(t *testing.T, tc testfixtures.CharacterizationTest, evalTime time.Time) (playbackshadow.ComparablePlaybackPlan, playbackshadow.ComparablePlaybackPlan, []string) {
	t.Helper()
	deps := newMockDeps()
	deps.scanner = &mockChannelScanner{found: true, capability: tc.SourceCap}
	deps.hostPressure = playbackprofile.HostPressureAssessment{EffectiveBand: tc.HostPressure}
	svc := NewService(deps)

	params := make(map[string]string, len(tc.Params)+1)
	for key, value := range tc.Params {
		params[key] = value
	}
	params["profile"] = "compatible"

	caps := capabilities.ResolveRuntimeProbeCapabilities(capabilities.PlaybackCapabilities{
		ClientFamilyFallback: tc.ClientFam,
		NetworkContext: &capabilities.NetworkContext{
			DownlinkKbps: tc.NetworkKbps,
		},
	})
	intent := Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-" + tc.Name,
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        params,
		CorrelationID: "corr-" + tc.Name,
		Mode:          tc.Mode,
		UserAgent:     "unit-test",
		ClientCaps:    &caps,
		Logger:        zerolog.Nop(),
	}

	res, intentErr := svc.ProcessIntent(context.Background(), intent)
	if intentErr != nil {
		t.Fatalf("ProcessIntent failed: %v", intentErr)
	}
	legacyPlan := playbackshadow.ComparablePlaybackPlan{IsValid: true, Outcome: "deny"}
	if res.Status == "accepted" {
		profile := deps.store.putSession.Profile
		legacyPlan = playbackshadow.ComparableFromLegacySession(deps.store.putSession.PlaybackTrace, &profile)
	}

	scope := "live"
	if tc.Mode == model.ModeRecording {
		scope = "recording"
	}
	disableTranscode := intent.Params["allow_transcode"] == "0"
	legacyInput := playbackshadow.LegacyPlanningInput{
		EvaluatedAt:        evalTime.UnixMilli(),
		Scope:              scope,
		RequestedIntent:    string(intent.Type),
		SourceIdentity:     "mock-source",
		ClientFamily:       tc.ClientFam,
		DownlinkKbps:       tc.NetworkKbps,
		RTTMillis:          tc.NetworkRTT,
		Container:          tc.SourceCap.Container,
		VideoCodec:         tc.SourceCap.VideoCodec,
		AudioCodec:         tc.SourceCap.AudioCodec,
		Width:              tc.SourceCap.Width,
		Height:             tc.SourceCap.Height,
		FPS:                int(tc.SourceCap.FPS),
		Interlaced:         tc.SourceCap.Interlaced,
		HostPressureBand:   string(tc.HostPressure),
		DisableTranscoding: disableTranscode,
		Confidence:         "ok",
		ObservedAt:         evalTime.UnixMilli(),
		NetworkCaptureTime: evalTime.UnixMilli(),
	}
	if tc.TruthConfidence > 0 && tc.TruthConfidence < 0.5 {
		legacyInput.Confidence = "stale"
	}
	legacyInput.SupportedContainers = append([]string(nil), caps.Containers...)
	legacyInput.SupportedVideoCodecs = append([]string(nil), caps.VideoCodecs...)
	legacyInput.SupportedAudioCodecs = append([]string(nil), caps.AudioCodecs...)
	legacyInput.SupportedEngines = append([]string(nil), caps.HLSEngines...)
	legacyInput.PreferredEngine = caps.PreferredHLSEngine
	legacyInput.SupportsHls = caps.SupportsHLS
	legacyInput.SupportsRange = caps.SupportsRange
	legacyInput.DeviceType = caps.DeviceType
	legacyInput.AllowTranscode = true
	if caps.AllowTranscode != nil {
		legacyInput.AllowTranscode = *caps.AllowTranscode
	}
	if caps.MaxVideo != nil {
		legacyInput.MaxVideoWidth = caps.MaxVideo.Width
		legacyInput.MaxVideoHeight = caps.MaxVideo.Height
		legacyInput.MaxVideoFPS = caps.MaxVideo.Fps
	}

	evidence, buildErr := playbackshadow.BuildPlaybackEvidence(legacyInput)
	if buildErr != nil {
		t.Fatalf("BuildPlaybackEvidence failed: %v", buildErr)
	}
	planResult, planErr := playbackplanner.Plan(evidence)
	if planErr != nil {
		t.Fatalf("Plan() returned error: %v", planErr)
	}
	plannerPlan := playbackshadow.ComparableFromPlanner(planResult.Plan)
	return legacyPlan, plannerPlan, playbackshadow.DiffComparablePlans(legacyPlan, plannerPlan)
}

func equalStringSets(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
}
