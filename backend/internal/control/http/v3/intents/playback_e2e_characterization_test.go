package intents

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/http/v3/intents/testfixtures"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

// TestPlaybackPlanner_Characterization runs the standard 10 E2E scenarios.
// Instead of defining the scenarios here, we use the shared testfixtures package
// so that shadow_test.go can run equivalence checks without duplication.

func RunCharacterizationTest(t *testing.T, tc testfixtures.CharacterizationTest) (*model.PlaybackTrace, *ports.ProfileSpec) {
	deps := newMockDeps()
	deps.scanner = &mockChannelScanner{found: true, capability: tc.SourceCap}
	deps.hostPressure = playbackprofile.HostPressureAssessment{EffectiveBand: tc.HostPressure}
	svc := NewService(deps)

	params := tc.Params
	if params == nil {
		params = make(map[string]string)
	}
	params["profile"] = "compatible"

	intent := Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-" + tc.Name,
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        params,
		CorrelationID: "corr-" + tc.Name,
		Mode:          tc.Mode,
		UserAgent:     "unit-test",
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: tc.ClientFam,
			NetworkContext: &capabilities.NetworkContext{
				DownlinkKbps: tc.NetworkKbps,
			},
		},
		Logger: zerolog.Nop(),
	}

	res, err := svc.ProcessIntent(context.Background(), intent)
	if err != nil {
		t.Fatalf("ProcessIntent failed: %v", err)
	}
	if res.Status != "accepted" {
		if tc.WantOutcome == "deny" {
			return nil, nil // Intent was denied, no trace/prof
		}
		t.Fatalf("expected accepted, got %s", res.Status)
	}

	if tc.WantOutcome == "deny" {
		t.Fatalf("expected deny, got accepted")
	}

	trace := deps.store.putSession.PlaybackTrace
	prof := deps.store.putSession.Profile

	if prof.Name != tc.WantProfile {
		t.Errorf("Profile.Name = %q, want %q", prof.Name, tc.WantProfile)
	}
	if prof.VideoCodec != tc.WantVideoCodec {
		t.Errorf("Profile.VideoCodec = %q, want %q", prof.VideoCodec, tc.WantVideoCodec)
	}
	if prof.Container != tc.WantContainer {
		t.Errorf("Profile.Container = %q, want %q", prof.Container, tc.WantContainer)
	}
	if trace.VideoQualityRung != tc.WantVideoRung {
		t.Errorf("Trace.VideoQualityRung = %q, want %q", trace.VideoQualityRung, tc.WantVideoRung)
	}
	if trace.ResolvedIntent != tc.WantResolved {
		t.Errorf("Trace.ResolvedIntent = %q, want %q", trace.ResolvedIntent, tc.WantResolved)
	}

	return trace, &prof
}

func TestPlaybackPlanner_Characterization(t *testing.T) {
	for _, tc := range testfixtures.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			RunCharacterizationTest(t, tc)
		})
	}
}
