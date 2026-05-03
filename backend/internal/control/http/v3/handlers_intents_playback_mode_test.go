package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/require"
)

type capturingIntentStore struct {
	lastSession *model.SessionRecord
}

func (s *capturingIntentStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	return nil, nil
}

func (s *capturingIntentStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	return nil, nil
}

func (s *capturingIntentStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	return nil, nil
}

func (s *capturingIntentStore) PutSessionWithIdempotency(ctx context.Context, session *model.SessionRecord, idempotencyKey string, expiration time.Duration) (string, bool, error) {
	cpy := *session
	s.lastSession = &cpy
	return "", false, nil
}

type noopIntentBus struct{}

func (b *noopIntentBus) Publish(ctx context.Context, topic string, payload interface{}) error {
	return nil
}

func (b *noopIntentBus) Subscribe(ctx context.Context, topic string) (bus.Subscriber, error) {
	return &dummySubscriber{}, nil
}

type noopIntentScanner struct{}

func (s *noopIntentScanner) GetCapability(ref string) (scan.Capability, bool) {
	return scan.Capability{}, true
}

func (s *noopIntentScanner) RunBackground() bool { return false }

func (s *noopIntentScanner) RunBackgroundForce() bool { return false }

type fixedIntentScanner struct {
	capability scan.Capability
}

func (s *fixedIntentScanner) GetCapability(ref string) (scan.Capability, bool) {
	return s.capability, true
}

func (s *fixedIntentScanner) RunBackground() bool { return false }

func (s *fixedIntentScanner) RunBackgroundForce() bool { return false }

type noopIntentPreflight struct{}

func (p *noopIntentPreflight) Check(ctx context.Context, ref preflight.SourceRef) (preflight.PreflightResult, error) {
	return preflight.PreflightResult{Outcome: preflight.PreflightOK}, nil
}

func TestHandleV3Intents_PlaybackModeMapsToH264FMP4ProfileWithoutCopyPath(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:8F:4:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-playback-mode",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "direct_stream",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "hlsjs",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileH264FMP4, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileH264FMP4, store.lastSession.ContextData["profile"])
}

func TestHandleV3Intents_PlaybackModeHLSJSDesktopSafariMapsToH264FMP4WithoutCopyPath(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:8F:4:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-hlsjs-safari",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "hlsjs",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "hlsjs",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
			model.CtxKeyClientFamily:  "safari_native",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileH264FMP4, store.lastSession.Profile.Name)
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, profiles.ProfileH264FMP4, store.lastSession.ContextData["profile"])
	require.Equal(t, "safari_native", store.lastSession.ContextData[model.CtxKeyClientFamily])
}

func TestHandleV3Intents_PlaybackModeNativeHLSMapsToSafariProfile(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:11:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, "safari", store.lastSession.Profile.Name)
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, "safari", store.lastSession.ContextData["profile"])
}

func TestHandleV3Intents_PlaybackModeNativeHLSPreservesSafariBrowserContainer(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:11:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-safari-ua",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, "safari", store.lastSession.Profile.Name)
	require.False(t, store.lastSession.Profile.TranscodeVideo)
	require.Equal(t, "mpegts", store.lastSession.Profile.Container)
	require.Equal(t, "safari", store.lastSession.ContextData["profile"])
}

func TestHandleV3Intents_PlaybackModeNativeHLSPreservesSafariBrowserInterlacedContainer(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &fixedIntentScanner{capability: scan.Capability{Interlaced: true}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:11:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-safari-ua-interlaced",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, "safari", store.lastSession.Profile.Name)
	require.True(t, store.lastSession.Profile.TranscodeVideo)
	require.True(t, store.lastSession.Profile.Deinterlace)
	require.Equal(t, "mpegts", store.lastSession.Profile.Container)
	require.Equal(t, "safari", store.lastSession.ContextData["profile"])
}

func TestHandleV3Intents_PlaybackModeNativeHLSUsesQualifiedHEVCForSafariNative(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &fixedIntentScanner{capability: scan.Capability{Interlaced: true}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:33:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-hevc",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
			model.CtxKeyClientFamily:  playbackprofile.ClientSafariNative,
			"codecs":                  "hevc,h264",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.ContextData["profile"])
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.True(t, store.lastSession.Profile.TranscodeVideo)
}

func TestHandleV3Intents_PlaybackModeNativeHLSUsesFMP4ForQualifiedHEVCOnIOSSafariNative(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &fixedIntentScanner{capability: scan.Capability{Interlaced: false}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:35:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-ios-hevc",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
			model.CtxKeyClientFamily:  playbackprofile.ClientIOSSafariNative,
			"codecs":                  "hevc,h264",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.ContextData["profile"])
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.True(t, store.lastSession.Profile.TranscodeVideo)
	require.Equal(t, "vaapi_encode_only", store.lastSession.Profile.HWAccel)
}

func TestHandleV3Intents_PlaybackModeNativeHLSUsesAV1FMP4ForRuntimeCapableIOSSafariNative(t *testing.T) {
	t.Setenv("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", "true")
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &fixedIntentScanner{capability: scan.Capability{Interlaced: false}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:36:6:85:C00000:0:0:0:"
	clientCaps := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Container:            []string{"mp4", "fmp4"},
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		AudioCodecs:          []string{"aac", "ac3"},
		SupportsHls:          boolPtr(true),
		ClientFamilyFallback: strPtr(playbackprofile.ClientIOSSafariNative),
		PreferredHlsEngine:   strPtr("native"),
		RuntimeProbeUsed:     boolPtr(true),
		RuntimeProbeVersion:  intPtr(2),
		DeviceType:           strPtr("iphone"),
		DeviceContext: &PlaybackDeviceContext{
			Model:     strPtr("iPhone 15 Pro A17 Pro"),
			OsName:    strPtr("ios"),
			OsVersion: strPtr("17.5"),
			Platform:  strPtr("iphone"),
		},
		VideoCodecSignals: &[]PlaybackVideoCodecSignal{
			{Codec: "av1", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
			{Codec: "hevc", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
			{Codec: "h264", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
		},
	}
	capHash := hashV3Capabilities(&clientCaps)

	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-ios-av1",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: capHash,
	}, auth.TestSecret())

	intentType := IntentRequestType("stream.start")
	reqBody := IntentRequest{
		Type:                  &intentType,
		ServiceRef:            &serviceRef,
		PlaybackDecisionToken: &token,
		Client:                &clientCaps,
		Params: &map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 capHash,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileAV1HW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileAV1HW, store.lastSession.ContextData["profile"])
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, "av1", store.lastSession.Profile.VideoCodec)
	require.True(t, store.lastSession.Profile.TranscodeVideo)
}

func TestHandleV3Intents_PlaybackModeNativeHLSRuntimeH264UsesHEVCBaselineFromClientFamily(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:44:6:85:C00000:0:0:0:"
	clientCaps := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Container:            []string{"mp4", "ts"},
		VideoCodecs:          []string{"h264"},
		AudioCodecs:          []string{"aac", "ac3"},
		SupportsHls:          boolPtr(true),
		ClientFamilyFallback: strPtr(playbackprofile.ClientSafariNative),
		PreferredHlsEngine:   strPtr("native"),
		RuntimeProbeUsed:     boolPtr(true),
		RuntimeProbeVersion:  intPtr(2),
	}
	capHash := hashV3Capabilities(&clientCaps)

	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-hevc-family-fallback",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: capHash,
	}, auth.TestSecret())

	intentType := IntentRequestType("stream.start")
	reqBody := IntentRequest{
		Type:                  &intentType,
		ServiceRef:            &serviceRef,
		PlaybackDecisionToken: &token,
		Client:                &clientCaps,
		Params: &map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 capHash,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
}

func TestHandleV3Intents_PlaybackModeNativeHLSRuntimeH264UsesHEVCFMP4OnIOSSafariNative(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:46:6:85:C00000:0:0:0:"
	clientCaps := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Container:            []string{"mp4", "ts"},
		VideoCodecs:          []string{"h264"},
		AudioCodecs:          []string{"aac", "ac3"},
		SupportsHls:          boolPtr(true),
		ClientFamilyFallback: strPtr(playbackprofile.ClientIOSSafariNative),
		PreferredHlsEngine:   strPtr("native"),
		RuntimeProbeUsed:     boolPtr(true),
		RuntimeProbeVersion:  intPtr(2),
	}
	capHash := hashV3Capabilities(&clientCaps)

	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-ios-h264",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: capHash,
	}, auth.TestSecret())

	intentType := IntentRequestType("stream.start")
	reqBody := IntentRequest{
		Type:                  &intentType,
		ServiceRef:            &serviceRef,
		PlaybackDecisionToken: &token,
		Client:                &clientCaps,
		Params: &map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 capHash,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
}

func TestHandleV3Intents_PlaybackModeNativeHLSRuntimeAV1HEVCHintsUseAV1ProfileForIOSH264Source(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan: &fixedIntentScanner{capability: scan.Capability{
			Container:  "ts",
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1920,
			Height:     1080,
			FPS:        25,
			Interlaced: false,
		}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:146:6:85:C00000:0:0:0:"
	clientCaps := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Container:            []string{"mp4", "ts", "fmp4"},
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		AudioCodecs:          []string{"aac", "ac3"},
		SupportsHls:          boolPtr(true),
		ClientFamilyFallback: strPtr(playbackprofile.ClientIOSSafariNative),
		PreferredHlsEngine:   strPtr("native"),
		RuntimeProbeUsed:     boolPtr(true),
		RuntimeProbeVersion:  intPtr(2),
		DeviceType:           strPtr("iphone"),
		DeviceContext: &PlaybackDeviceContext{
			Model:     strPtr("iPhone 15 Pro A17 Pro"),
			OsName:    strPtr("ios"),
			OsVersion: strPtr("17.5"),
			Platform:  strPtr("iphone"),
		},
		VideoCodecSignals: &[]PlaybackVideoCodecSignal{
			{Codec: "av1", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
			{Codec: "hevc", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
			{Codec: "h264", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
		},
	}
	capHash := hashV3Capabilities(&clientCaps)

	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-ios-h264-source-rich-caps",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: capHash,
	}, auth.TestSecret())

	intentType := IntentRequestType("stream.start")
	reqBody := IntentRequest{
		Type:                  &intentType,
		ServiceRef:            &serviceRef,
		PlaybackDecisionToken: &token,
		Client:                &clientCaps,
		Params: &map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 capHash,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileAV1HW, store.lastSession.Profile.Name)
	require.Equal(t, "av1", store.lastSession.Profile.VideoCodec)
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
}

func TestHandleV3Intents_PlaybackModeHLSJSRuntimeRichCodecsKeepHighProfileForChromiumH264Source(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan: &fixedIntentScanner{capability: scan.Capability{
			Container:  "ts",
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1920,
			Height:     1080,
			FPS:        25,
			Interlaced: false,
		}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:246:6:85:C00000:0:0:0:"
	clientCaps := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Container:            []string{"mp4", "ts", "fmp4"},
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		AudioCodecs:          []string{"aac", "ac3"},
		SupportsHls:          boolPtr(true),
		ClientFamilyFallback: strPtr(playbackprofile.ClientChromiumHLSJS),
		PreferredHlsEngine:   strPtr("hlsjs"),
		RuntimeProbeUsed:     boolPtr(true),
		RuntimeProbeVersion:  intPtr(2),
	}
	capHash := hashV3Capabilities(&clientCaps)

	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-hlsjs-chromium-h264-source-rich-caps",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "hlsjs",
		CapHash: capHash,
	}, auth.TestSecret())

	intentType := IntentRequestType("stream.start")
	reqBody := IntentRequest{
		Type:                  &intentType,
		ServiceRef:            &serviceRef,
		PlaybackDecisionToken: &token,
		Client:                &clientCaps,
		Params: &map[string]string{
			"playback_mode":           "hlsjs",
			"playback_decision_token": token,
			"capHash":                 capHash,
			"codecs":                  "av1,hevc,h264",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileHigh, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileHigh, store.lastSession.ContextData["profile"])
	require.False(t, store.lastSession.Profile.TranscodeVideo)
	require.NotEqual(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.NotEqual(t, "av1", store.lastSession.Profile.VideoCodec)
}

func TestHandleV3Intents_PlaybackModeNativeHLSRuntimeHEVCHintsKeepSafariProfileForDesktopH264Source(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan: &fixedIntentScanner{capability: scan.Capability{
			Container:  "ts",
			Interlaced: false,
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1280,
			Height:     720,
			FPS:        50,
		}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:132F:3EF:1:C00000:0:0:0:"
	clientCaps := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Container:            []string{"mp4", "ts"},
		VideoCodecs:          []string{"hevc", "h264"},
		AudioCodecs:          []string{"aac", "ac3", "mp3"},
		SupportsHls:          boolPtr(true),
		ClientFamilyFallback: strPtr(playbackprofile.ClientSafariNative),
		PreferredHlsEngine:   strPtr("native"),
		RuntimeProbeUsed:     boolPtr(true),
		RuntimeProbeVersion:  intPtr(2),
	}
	capHash := hashV3Capabilities(&clientCaps)

	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-desktop-h264-source",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: capHash,
	}, auth.TestSecret())

	intentType := IntentRequestType("stream.start")
	reqBody := IntentRequest{
		Type:                  &intentType,
		ServiceRef:            &serviceRef,
		PlaybackDecisionToken: &token,
		Client:                &clientCaps,
		Params: &map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 capHash,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.ContextData["profile"])
	require.Equal(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
}

func TestHandleV3Intents_PlaybackModeNativeHLSLegacySafariAliasRuntimeAV1UsesAV1Profile(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan: &fixedIntentScanner{capability: scan.Capability{
			Container:  "ts",
			Interlaced: false,
			VideoCodec: "h264",
			AudioCodec: "ac3",
			Width:      1920,
			Height:     1080,
			FPS:        25,
		}},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:EF75:3F9:1:C00000:0:0:0:"
	clientCaps := PlaybackCapabilities{
		CapabilitiesVersion:  3,
		Container:            []string{"mp4", "ts", "fmp4"},
		VideoCodecs:          []string{"av1", "hevc", "h264"},
		AudioCodecs:          []string{"aac", "ac3", "mp3"},
		SupportsHls:          boolPtr(true),
		ClientFamilyFallback: strPtr("safari"),
		PreferredHlsEngine:   strPtr("native"),
		RuntimeProbeUsed:     boolPtr(true),
		RuntimeProbeVersion:  intPtr(2),
		DeviceContext: &PlaybackDeviceContext{
			Model:     strPtr("MacBook Air M3"),
			OsName:    strPtr("macos"),
			OsVersion: strPtr("14.4"),
			Platform:  strPtr("macintel"),
		},
		VideoCodecSignals: &[]PlaybackVideoCodecSignal{
			{Codec: "av1", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
			{Codec: "hevc", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
			{Codec: "h264", Supported: true, Smooth: boolPtr(true), PowerEfficient: boolPtr(true)},
		},
	}
	capHash := hashV3Capabilities(&clientCaps)

	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-native-hls-legacy-safari-av1",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "native_hls",
		CapHash: capHash,
	}, auth.TestSecret())

	intentType := IntentRequestType("stream.start")
	reqBody := IntentRequest{
		Type:                  &intentType,
		ServiceRef:            &serviceRef,
		PlaybackDecisionToken: &token,
		Client:                &clientCaps,
		Params: &map[string]string{
			"playback_mode":           "native_hls",
			"playback_decision_token": token,
			"capHash":                 capHash,
			"client_family":           "safari",
			"preferred_hls_engine":    "native",
			"device_type":             "mac",
			"codecs":                  "av1,hevc,h264",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileAV1HW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileAV1HW, store.lastSession.ContextData["profile"])
	require.Equal(t, "av1", store.lastSession.Profile.VideoCodec)
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, playbackprofile.ClientSafariNative, store.lastSession.ContextData[model.CtxKeyClientFamily])
	require.Equal(t, playbackprofile.ClientSafariNative, store.lastSession.PlaybackTrace.Client.ClientFamily)
}

func TestHandleV3Intents_PlaybackModeTranscodeUsesMeasuredCodecRanking(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:22:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-transcode-codecs",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "transcode",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "transcode",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
			"codecs":                  "hevc,h264",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.ContextData["profile"])
}

func TestHandleV3Intents_PlaybackModeTranscodeUsesEncodeOnlyHEVCForIOSSafari(t *testing.T) {
	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:23:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-transcode-ios-hevc",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "transcode",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "transcode",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
			model.CtxKeyClientFamily:  playbackprofile.ClientIOSSafariNative,
			"codecs":                  "hevc,h264",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.ContextData["profile"])
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.True(t, store.lastSession.Profile.TranscodeVideo)
	require.Equal(t, "vaapi_encode_only", store.lastSession.Profile.HWAccel)
}

func TestHandleV3Intents_PlaybackModeTranscodeClampsIOSSafariParamsToHEVCFMP4(t *testing.T) {
	t.Setenv("XG2G_EXPERIMENTAL_AV1_MPEGTS_ENABLED", "true")

	store := &capturingIntentStore{}
	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0}
	cfg.Engine.Enabled = true
	cfg.Limits.MaxSessions = 8
	cfg.Limits.MaxTranscodes = 4
	cfg.Sessions.LeaseTTL = time.Minute
	cfg.Sessions.HeartbeatInterval = 30 * time.Second
	cfg.Enigma2.BaseURL = "http://example.com"

	hardware.SetVAAPIPreflightResult(true)
	hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{
		"h264_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 90 * time.Millisecond},
		"hevc_vaapi": {Verified: true, AutoEligible: true, ProbeElapsed: 40 * time.Millisecond},
		"av1_vaapi":  {Verified: true, AutoEligible: true, ProbeElapsed: 30 * time.Millisecond},
	})
	t.Cleanup(func() {
		hardware.SetVAAPIPreflightResult(false)
		hardware.SetVAAPIEncoderCapabilities(map[string]hardware.VAAPIEncoderCapability{})
	})

	s := &Server{
		cfg:       cfg,
		JWTSecret: auth.TestSecret(),
	}
	s.SetDependencies(Dependencies{
		Bus:   &noopIntentBus{},
		Store: store,
		Scan:  &noopIntentScanner{},
	})
	s.admission = admission.NewController(cfg)
	s.admissionState = &MockAdmissionState{Tuners: 1}

	serviceRef := "1:0:19:24:6:85:C00000:0:0:0:"
	now := time.Now().Unix()
	token := generateTestToken(t, auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(serviceRef),
		Jti:     "test-uuid-transcode-ios-av1",
		Iat:     now,
		Nbf:     now - 10,
		Exp:     now + 60,
		Mode:    "transcode",
		CapHash: "cap-match",
	}, auth.TestSecret())

	reqBody := v3api.IntentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: &token,
		Params: map[string]string{
			"playback_mode":           "transcode",
			"playback_decision_token": token,
			"capHash":                 "cap-match",
			model.CtxKeyClientFamily:  playbackprofile.ClientIOSSafariNative,
			"codecs":                  "av1,hevc,h264",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1")
	rr := httptest.NewRecorder()

	s.handleV3Intents(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	require.NotNil(t, store.lastSession)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.Profile.Name)
	require.Equal(t, profiles.ProfileSafariHEVCHW, store.lastSession.ContextData["profile"])
	require.Equal(t, "fmp4", store.lastSession.Profile.Container)
	require.Equal(t, "hevc", store.lastSession.Profile.VideoCodec)
	require.True(t, store.lastSession.Profile.TranscodeVideo)
}
