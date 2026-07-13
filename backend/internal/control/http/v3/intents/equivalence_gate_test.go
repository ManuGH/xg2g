package intents

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/intents/testfixtures"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/rs/zerolog"
)

func TestEquivalenceGate(t *testing.T) {
	evalTime := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)

	for _, tc := range testfixtures.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			// 1. Run genuine legacy path
			deps := newMockDeps()
			deps.scanner = &mockChannelScanner{found: true, capability: tc.SourceCap}
			deps.hostPressure = playbackprofile.HostPressureAssessment{EffectiveBand: tc.HostPressure}
			svc := NewService(deps)

			params := make(map[string]string)
			if tc.Params != nil {
				for k, v := range tc.Params {
					params[k] = v
				}
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

			// 2. Build PlaybackEvidence for the new planner using the exact same inputs
			scope := "live"
			if tc.Mode == model.ModeRecording {
				scope = "recording"
			}

			maxQ := ""
			disableTranscode := false
			if intent.Params["allow_transcode"] == "0" {
				disableTranscode = true
			}

			legacyInput := playbackshadow.LegacyPlanningInput{
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
				MaxQualityRung:     maxQ,
				DisableTranscoding: disableTranscode,
				MaxGlobalBitrate:   0,
				StrictFreshness:    false,
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
				legacyInput.AllowTranscode = true // default to true in legacy? No, wait, if nil, it's allowed unless disabled above.
			}
			if caps.MaxVideo != nil {
				legacyInput.MaxVideoWidth = caps.MaxVideo.Width
				legacyInput.MaxVideoHeight = caps.MaxVideo.Height
				legacyInput.MaxVideoFPS = caps.MaxVideo.Fps
			}

			if tc.TruthConfidence == 0 {
				legacyInput.Confidence = "ok" // Default to full confidence if not set
			} else if tc.TruthConfidence < 0.5 {
				legacyInput.Confidence = "stale"
			} else {
				legacyInput.Confidence = "ok"
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

			newComp := playbackshadow.ComparableFromPlanner(planRes.Plan)

			// 4. Compare
			diffs := playbackshadow.DiffComparablePlans(legacyPlan, newComp)
			if len(diffs) > 0 {
				t.Errorf("Equivalence check failed. Unexpected Diffs: %v\n\tLegacy: %+v\n\tNew:    %+v", diffs, legacyPlan, newComp)
			}
		})
	}
}
