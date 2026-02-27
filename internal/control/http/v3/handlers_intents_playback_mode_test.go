package v3

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/metrics"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPlaybackModeIntentServer(t *testing.T) *Server {
	t.Helper()

	cfg := config.AppConfig{
		Engine: config.EngineConfig{
			Enabled:    true,
			TunerSlots: []int{0, 1},
		},
		Limits: config.LimitsConfig{
			MaxSessions:   10,
			MaxTranscodes: 5,
		},
	}

	s := NewServer(cfg, nil, func() {})
	s.v3Bus = bus.NewMemoryBus()
	s.v3Store = &MockStoreForStreams{}
	s.admissionState = &MockAdmissionState{
		Tuners:     2,
		Sessions:   0,
		Transcodes: 0,
	}
	return s
}

func postStreamStartIntent(t *testing.T, s *Server, serviceRef string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	reqBody := v3api.IntentRequest{
		Type:       model.IntentTypeStreamStart,
		ServiceRef: serviceRef,
		Params:     params,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleV3Intents(w, req)
	return w
}

func TestHandleV3Intents_PlaybackModeAttestationRequired(t *testing.T) {
	s := newPlaybackModeIntentServer(t)
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"

	w := postStreamStartIntent(t, s, serviceRef, map[string]string{
		"playback_mode": "hlsjs",
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "playback_decision_id or playback_decision_token is required")
}

func TestHandleV3Intents_PlaybackModeBothKeysEqualAccepted(t *testing.T) {
	s := newPlaybackModeIntentServer(t)
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"
	token := s.attestLivePlaybackDecision("req-equal", "", serviceRef, "hlsjs")
	require.NotEmpty(t, token)

	before := getLiveIntentPlaybackKeyCounterValue(t, "both", "equal")
	w := postStreamStartIntent(t, s, serviceRef, map[string]string{
		"playback_mode":           "hlsjs",
		"playback_decision_id":    token,
		"playback_decision_token": token,
	})

	require.Equal(t, http.StatusAccepted, w.Code)
	after := getLiveIntentPlaybackKeyCounterValue(t, "both", "equal")
	assert.Equal(t, before+1, after)
}

func TestHandleV3Intents_PlaybackModeBothKeysMismatchRejected(t *testing.T) {
	s := newPlaybackModeIntentServer(t)
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"
	token := s.attestLivePlaybackDecision("req-mismatch", "", serviceRef, "hlsjs")
	require.NotEmpty(t, token)

	before := getLiveIntentPlaybackKeyCounterValue(t, "both", "mismatch")
	w := postStreamStartIntent(t, s, serviceRef, map[string]string{
		"playback_mode":           "hlsjs",
		"playback_decision_id":    token,
		"playback_decision_token": token + "-different",
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "playback_decision_id and playback_decision_token mismatch")
	after := getLiveIntentPlaybackKeyCounterValue(t, "both", "mismatch")
	assert.Equal(t, before+1, after)
}

func TestHandleV3Intents_PlaybackModeDeprecatedKeyAcceptedAndMetered(t *testing.T) {
	s := newPlaybackModeIntentServer(t)
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"
	token := s.attestLivePlaybackDecision("req-deprecated", "", serviceRef, "hlsjs")
	require.NotEmpty(t, token)

	before := getLiveIntentPlaybackKeyCounterValue(t, "playback_decision_id", "accepted")
	w := postStreamStartIntent(t, s, serviceRef, map[string]string{
		"playback_mode":        "hlsjs",
		"playback_decision_id": token,
	})

	require.Equal(t, http.StatusAccepted, w.Code)
	after := getLiveIntentPlaybackKeyCounterValue(t, "playback_decision_id", "accepted")
	assert.Equal(t, before+1, after)
}

func TestHandleV3Intents_PlaybackModeAttestationMismatchRejected(t *testing.T) {
	s := newPlaybackModeIntentServer(t)
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"
	token := s.attestLivePlaybackDecision("req-1", "", serviceRef, "native_hls")
	require.NotEmpty(t, token)

	w := postStreamStartIntent(t, s, serviceRef, map[string]string{
		"playback_mode":        "hlsjs",
		"playback_decision_id": token,
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "playback_mode is not attested")
}

func TestHandleV3Intents_PlaybackModeAttestationAccepted(t *testing.T) {
	s := newPlaybackModeIntentServer(t)
	serviceRef := "1:0:1:1337:42:99:0:0:0:0:"
	token := s.attestLivePlaybackDecision("req-2", "", serviceRef, "hlsjs")
	require.NotEmpty(t, token)

	w := postStreamStartIntent(t, s, serviceRef, map[string]string{
		"playback_mode":        "hlsjs",
		"playback_decision_id": token,
	})

	require.Equal(t, http.StatusAccepted, w.Code)

	var resp v3api.IntentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "accepted", resp.Status)
	assert.NotEmpty(t, resp.SessionID)
}

func getLiveIntentPlaybackKeyCounterValue(t *testing.T, key, result string) float64 {
	t.Helper()

	metric, err := metrics.LiveIntentsPlaybackKeyTotal.GetMetricWithLabelValues(key, result)
	require.NoError(t, err)

	promMetric := &dto.Metric{}
	require.NoError(t, metric.(prometheus.Metric).Write(promMetric))
	return promMetric.GetCounter().GetValue()
}
