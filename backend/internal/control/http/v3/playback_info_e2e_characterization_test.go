package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/config"
	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// characterizationTest validates the HTTP boundary (PostLivePlaybackInfo/PostRecordingPlaybackInfo)
type httpCharacterizationTest struct {
	name         string
	mode         string // "live" or "recording"
	sourceCap    scan.Capability
	truth        playback.MediaTruth
	capabilities string
	hostPressure playbackprofile.HostPressureBand

	wantDecisionMode string
	wantEngine       string
	wantContainer    string
	wantVideoCodec   string
	wantTokenMode    string
}

func runHTTPCharacterizationTest(t *testing.T, tc httpCharacterizationTest) {
	svc := new(MockRecordingsService)
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"
	if tc.mode == "recording" {
		serviceRef = "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie.ts"
	}
	recordingID := recservice.EncodeRecordingID(serviceRef)

	if tc.mode == "recording" {
		svc.On("GetMediaTruth", mock.Anything, recordingID).Return(tc.truth, nil)
	}

	cfg := config.AppConfig{}
	cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
	cfg.HLS.Root = "/tmp/hls"

	s := &Server{
		cfg:               cfg,
		recordingsService: svc,
		JWTSecret:         []byte("test-secret-key-1234567890123456"),
	}

	if tc.mode == "live" {
		s.SetDependencies(Dependencies{
			Scan: &fixedPlaybackInfoScanner{
				found:      true,
				capability: tc.sourceCap,
			},
			RecordingsService: svc,
		})
	}

	if tc.hostPressure != "" {
		s.admissionState = &MockAdmissionState{Tuners: 2, Sessions: 0, Transcodes: 0}
		s.hostPressureTracker = hardware.NewPressureTracker()
		s.hostPressureMonitor = admissionmonitor.NewResourceMonitor(8, 2, 1.5)
		if tc.hostPressure == playbackprofile.HostPressureConstrained {
			for i := 0; i < 5; i++ {
				s.hostPressureMonitor.ObserveCPULoad(999)
			}
		}
	}

	body := `{"serviceRef":"` + serviceRef + `", "capabilities": ` + tc.capabilities + `}`

	var req *http.Request
	if tc.mode == "live" {
		req = httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info?profile=quality", strings.NewReader(body))
	} else {
		req = httptest.NewRequest(http.MethodPost, "/api/v3/recordings/"+recordingID+"/stream-info", strings.NewReader(tc.capabilities))
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	if tc.mode == "live" {
		s.PostLivePlaybackInfo(w, req)
	} else {
		s.PostRecordingPlaybackInfo(w, req, recordingID)
	}

	require.Equal(t, http.StatusOK, w.Code, "expected 200 OK, got body: %s", w.Body.String())

	var raw map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	require.NoError(t, err)

	dec, ok := raw["decision"].(map[string]any)
	require.True(t, ok)

	assert.Equal(t, tc.wantDecisionMode, dec["mode"], "Decision Mode mismatch")

	if tc.wantEngine != "" {
		assert.Equal(t, tc.wantEngine, dec["selectedOutputKind"], "Output Engine mismatch")
	}

	if tc.wantContainer != "" {
		selected, ok := dec["selected"].(map[string]any)
		if assert.True(t, ok, "missing selected block") {
			assert.Equal(t, tc.wantContainer, selected["container"], "Container mismatch")
			if tc.wantVideoCodec != "" {
				assert.Equal(t, tc.wantVideoCodec, selected["videoCodec"], "VideoCodec mismatch")
			}
		}
	}

	// Validate Token
	tokenStr, ok := raw["playbackDecisionToken"].(string)
	if tc.wantTokenMode == "deny" {
		assert.False(t, ok, "Deny should not return a token")
	} else if tc.wantTokenMode == "none" {
		assert.False(t, ok, "Expected no token to be returned")
	} else {
		require.True(t, ok, "Missing playbackDecisionToken")
		claims, err := v3auth.VerifyStrict(tokenStr, s.JWTSecret, "xg2g/v3/intents", "xg2g")
		require.NoError(t, err, "Token validation failed")
		assert.Equal(t, tc.wantTokenMode, claims.Mode, "Token Mode mismatch")
	}
}

func TestHTTPBoundary_Characterization(t *testing.T) {
	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 10},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(nil)
	})

	cases := []httpCharacterizationTest{
		{
			name:             "1_Safari_Native_H264",
			mode:             "live",
			sourceCap:        scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			capabilities:     `{"clientFamilyFallback":"safari_native", "container":["mp4","ts","mpegts","hls"], "videoCodecs":["h264"], "audioCodecs":["aac"], "preferredHlsEngine":"native", "hlsEngines":["native"]}`,
			wantDecisionMode: "direct_stream",
			wantEngine:       "hls",
			wantContainer:    "ts",
			wantTokenMode:    "direct_stream",
		},
		{
			name:             "2_Safari_Native_HEVC_4K",
			mode:             "live",
			sourceCap:        scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "hevc", AudioCodec: "aac", Width: 3840, Height: 2160, FPS: 50},
			capabilities:     `{"capabilitiesVersion": 3, "clientFamilyFallback":"safari_native", "container":["mp4","fmp4","hls"], "videoCodecs":["hevc","h264"], "audioCodecs":["aac"], "preferredHlsEngine":"native", "hlsEngines":["native"]}`,
			wantDecisionMode: "direct_stream",
			wantEngine:       "hls",
			wantContainer:    "fmp4",
			wantTokenMode:    "direct_stream",
		},
		{
			name:             "3_iOS_Safari",
			mode:             "live",
			sourceCap:        scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1280, Height: 720, FPS: 50},
			capabilities:     `{"clientFamilyFallback":"ios_safari_native", "container":["mp4","ts","fmp4","hls"], "videoCodecs":["h264"], "audioCodecs":["aac"], "preferredHlsEngine":"native", "hlsEngines":["native"]}`,
			wantDecisionMode: "direct_stream",
			wantEngine:       "hls",
			wantContainer:    "ts",
			wantTokenMode:    "direct_stream",
		},
		{
			name:             "4_Chromium_HLSJS",
			mode:             "live",
			sourceCap:        scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			capabilities:     `{"capabilitiesVersion": 3, "clientFamilyFallback":"chromium", "container":["mp4","fmp4","hls"], "videoCodecs":["h264"], "audioCodecs":["aac"], "preferredHlsEngine":"hls.js", "hlsEngines":["hls.js"]}`,
			wantDecisionMode: "direct_stream",
			wantEngine:       "hls",
			wantContainer:    "ts",
			wantTokenMode:    "direct_stream",
		},
		{
			name:             "5_Constrained_WAN_Fallback",
			mode:             "live",
			sourceCap:        scan.Capability{State: scan.CapabilityStateOK, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			hostPressure:     playbackprofile.HostPressureConstrained,
			capabilities:     `{"capabilitiesVersion": 3, "clientFamilyFallback":"chromium", "container":["mp4","fmp4","hls"], "videoCodecs":["h264"], "audioCodecs":["aac"], "preferredHlsEngine":"hls.js", "hlsEngines":["hls.js"], "networkContext": {"downlinkKbps": 1000}}`,
			wantDecisionMode: "direct_stream",
			wantEngine:       "hls",
			wantContainer:    "ts",
			wantTokenMode:    "direct_stream",
		},
		{
			name:             "6_Dirty_DVB_Fallback",
			mode:             "live",
			sourceCap:        scan.Capability{State: scan.CapabilityStatePartial, Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 25, Interlaced: true},
			capabilities:     `{"clientFamilyFallback":"chromium", "container":["mp4","fmp4","hls"], "videoCodecs":["h264"], "audioCodecs":["aac"], "preferredHlsEngine":"hls.js", "hlsEngines":["hls.js"]}`,
			wantDecisionMode: "transcode",
			wantEngine:       "hls",
			wantContainer:    "fmp4",
			wantTokenMode:    "transcode",
		},
		{
			name:             "7_Recording_Playback",
			mode:             "recording",
			truth:            playback.MediaTruth{Container: "mpegts", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 50},
			capabilities:     `{"capabilitiesVersion": 3, "clientFamilyFallback":"chromium", "container":["mp4","fmp4","hls"], "videoCodecs":["h264"], "audioCodecs":["aac"], "preferredHlsEngine":"hls.js", "hlsEngines":["hls.js"]}`,
			wantDecisionMode: "direct_stream",
			wantEngine:       "hls",
			wantContainer:    "mpegts",
			wantTokenMode:    "none",
		},
		{
			name:             "8_Deny_Scenario",
			mode:             "recording",
			truth:            playback.MediaTruth{Container: "mpegts", VideoCodec: "mpeg2video", AudioCodec: "mp2", Width: 720, Height: 576, FPS: 25},
			capabilities:     `{"capabilitiesVersion": 3, "clientFamilyFallback":"safari_native", "container":["mp4"], "videoCodecs":["hevc"], "audioCodecs":["aac"], "allowTranscode": false}`,
			wantDecisionMode: "deny",
			wantTokenMode:    "deny",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runHTTPCharacterizationTest(t, tc)
		})
	}
}

func TestHTTPBoundary_EpgBadgeIsPassiveAndCannotAuthorizeStart(t *testing.T) {
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"
	svc := new(MockRecordingsService)
	cfg := config.AppConfig{}
	cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
	cfg.HLS.Root = "/tmp/hls"
	s := &Server{
		cfg:               cfg,
		recordingsService: svc,
		JWTSecret:         []byte("test-secret-key-1234567890123456"),
		// Exercise the strongest enforcement mode. The passive response must
		// remain available, but it must not carry start authority.
		plannerReceiptEnabled:  true,
		plannerReceiptRequired: true,
	}
	s.SetDependencies(Dependencies{
		Scan: &fixedPlaybackInfoScanner{
			found: true,
			capability: scan.Capability{
				State:      scan.CapabilityStateOK,
				Container:  "mpegts",
				VideoCodec: "h264",
				AudioCodec: "aac",
				Width:      1920,
				Height:     1080,
				FPS:        25,
			},
		},
		RecordingsService: svc,
	})

	body := `{"serviceRef":"` + serviceRef + `","capabilities":{"capabilitiesVersion":3,"container":["mp4","ts","fmp4"],"videoCodecs":["h264"],"audioCodecs":["aac"],"supportsHls":true,"hlsEngines":["native"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(v3recordings.PlaybackInfoContextHeader, v3recordings.PlaybackInfoContextEpgBadge)
	w := httptest.NewRecorder()

	s.PostLivePlaybackInfo(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	require.NotNil(t, raw["decision"])
	_, hasToken := raw["playbackDecisionToken"]
	require.False(t, hasToken, "passive EPG response must not authorize stream.start")
}
