package intents

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/rs/zerolog"
)

// characterizationTest validates the legacy intent system's final PlaybackTrace and Profile
// outcomes to freeze them in place before migrating to the pure PlaybackPlanner.
type characterizationTest struct {
	name             string
	mode             string
	sourceCap        scan.Capability
	clientFam        string
	hostPressure     playbackprofile.HostPressureBand
	params           map[string]string
	wantProfile      string
	wantVideoRung    string
	wantVideoCodec   string
	wantContainer    string
	wantResolved     string
}

func runCharacterizationTest(t *testing.T, tc characterizationTest) {
	deps := newMockDeps()
	deps.scanner = &mockChannelScanner{found: true, capability: tc.sourceCap}
	deps.hostPressure = playbackprofile.HostPressureAssessment{EffectiveBand: tc.hostPressure}
	svc := NewService(deps)

	params := tc.params
	if params == nil {
		params = make(map[string]string)
	}
	params["profile"] = "compatible"

	intent := Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-" + tc.name,
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        params,
		CorrelationID: "corr-" + tc.name,
		Mode:          tc.mode,
		UserAgent:     "unit-test",
		ClientCaps: &capabilities.PlaybackCapabilities{
			ClientFamilyFallback: tc.clientFam,
		},
		Logger: zerolog.Nop(),
	}

	res, err := svc.ProcessIntent(context.Background(), intent)
	if err != nil {
		t.Fatalf("ProcessIntent failed: %v", err)
	}
	if res.Status != "accepted" {
		t.Fatalf("expected accepted, got %s", res.Status)
	}

	trace := deps.store.putSession.PlaybackTrace
	prof := deps.store.putSession.Profile

	if prof.Name != tc.wantProfile {
		t.Errorf("Profile.Name = %q, want %q", prof.Name, tc.wantProfile)
	}
	if prof.VideoCodec != tc.wantVideoCodec {
		t.Errorf("Profile.VideoCodec = %q, want %q", prof.VideoCodec, tc.wantVideoCodec)
	}
	if prof.Container != tc.wantContainer {
		t.Errorf("Profile.Container = %q, want %q", prof.Container, tc.wantContainer)
	}
	if trace.VideoQualityRung != tc.wantVideoRung {
		t.Errorf("Trace.VideoQualityRung = %q, want %q", trace.VideoQualityRung, tc.wantVideoRung)
	}
	if trace.ResolvedIntent != tc.wantResolved {
		t.Errorf("Trace.ResolvedIntent = %q, want %q", trace.ResolvedIntent, tc.wantResolved)
	}
}

func TestPlaybackPlanner_Characterization(t *testing.T) {
	cases := []characterizationTest{
		{
			name:      "1_Safari_Native_H264",
			mode:      model.ModeLive,
			sourceCap: scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			clientFam: playbackprofile.ClientSafariNative,
			wantProfile: "high",
			wantVideoRung: "",
			wantVideoCodec: "",
			wantContainer: "",
			wantResolved: "compatible",
		},
		{
			name:      "2_Safari_Native_HEVC_4K",
			mode:      model.ModeLive,
			sourceCap: scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "hevc", AudioCodec: "aac", Width: 3840, Height: 2160, FPS: 50},
			clientFam: playbackprofile.ClientSafariNative,
			params:    map[string]string{"native_hevc_safari": "1"},
			wantProfile: "high",
			wantVideoRung: "",
			wantVideoCodec: "",
			wantContainer: "",
			wantResolved: "compatible",
		},
		{
			name:      "3_iOS_Safari",
			mode:      model.ModeLive,
			sourceCap: scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1280, Height: 720, FPS: 50},
			clientFam: playbackprofile.ClientIOSSafariNative,
			wantProfile: "high",
			wantVideoRung: "",
			wantVideoCodec: "",
			wantContainer: "",
			wantResolved: "compatible",
		},
		{
			name:      "4_Chromium_HLSJS",
			mode:      model.ModeLive,
			sourceCap: scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			clientFam: playbackprofile.ClientChromiumHLSJS,
			wantProfile: "high",
			wantVideoRung: "",
			wantVideoCodec: "",
			wantContainer: "",
			wantResolved: "compatible",
		},
		{
			name:      "5_Constrained_WAN_Fallback",
			mode:      model.ModeLive,
			sourceCap: scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			clientFam: playbackprofile.ClientChromiumHLSJS,
			hostPressure: playbackprofile.HostPressureConstrained,
			wantProfile: "high",
			wantVideoRung: "",
			wantVideoCodec: "",
			wantContainer: "",
			wantResolved: "compatible",
		},
		{
			name:      "6_Dirty_DVB_Fallback",
			mode:      model.ModeLive,
			sourceCap: scan.Capability{State: scan.CapabilityStatePartial, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 25, Interlaced: true},
			clientFam: playbackprofile.ClientChromiumHLSJS,
			wantProfile: "high",
			wantVideoRung: "",
			wantVideoCodec: "",
			wantContainer: "",
			wantResolved: "compatible",
		},
		{
			name:      "7_Recording_Playback",
			mode:      model.ModeRecording,
			sourceCap: scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			clientFam: playbackprofile.ClientChromiumHLSJS,
			wantProfile: "high",
			wantVideoRung: "",
			wantVideoCodec: "",
			wantContainer: "",
			wantResolved: "compatible",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCharacterizationTest(t, tc)
		})
	}
}
