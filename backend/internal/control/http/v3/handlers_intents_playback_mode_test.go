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
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
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

type noopIntentPreflight struct{}

func (p *noopIntentPreflight) Check(ctx context.Context, ref preflight.SourceRef) (preflight.PreflightResult, error) {
	return preflight.PreflightResult{Outcome: preflight.PreflightOK}, nil
}

func TestHandleV3Intents_PlaybackModeMapsToHighProfile(t *testing.T) {
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
	require.Equal(t, "high", store.lastSession.Profile.Name)
	require.Equal(t, "high", store.lastSession.ContextData["profile"])
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
	require.Equal(t, "safari", store.lastSession.ContextData["profile"])
}
