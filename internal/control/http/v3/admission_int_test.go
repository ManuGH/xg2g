package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAdmissionState implements AdmissionStateSource for testing
type MockAdmissionState struct {
	Tuners     int
	Sessions   int
	Transcodes int
	Err        error
}

func (m *MockAdmissionState) Snapshot(ctx context.Context) (admission.RuntimeState, error) {
	if m.Err != nil {
		return admission.RuntimeState{}, m.Err
	}
	return admission.RuntimeState{
		TunerSlots:       m.Tuners,
		SessionsActive:   m.Sessions,
		TranscodesActive: m.Transcodes,
	}, nil
}

func TestAdmissionIntegration(t *testing.T) {
	// Base Config (Allow everything initially)
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

	// Setup Server dependencies
	bus := bus.NewMemoryBus()
	store := &MockStoreForStreams{} // Use existing mock from streams_test

	setupServer := func(state *MockAdmissionState) *Server {
		s := NewServer(cfg, nil, func() {})
		s.v3Bus = bus
		s.v3Store = store
		s.admissionState = state
		return s
	}

	t.Run("Engine Disabled -> 503", func(t *testing.T) {
		disabledCfg := cfg
		disabledCfg.Engine.Enabled = false
		s := setupServer(&MockAdmissionState{})
		s.cfg = disabledCfg
		s.admission = admission.NewController(disabledCfg) // Re-init controller with disabled config

		reqBody := api.IntentRequest{
			Type:       model.IntentTypeStreamStart,
			ServiceRef: "1:0:1:0:0:0:0:0:0:0:http://example.com/stream",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v3/intents", bytes.NewReader(body))
		w := httptest.NewRecorder()

		s.handleV3Intents(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var pd ProblemDetails
		json.NewDecoder(resp.Body).Decode(&pd)
		require.NotNil(t, pd.Code)
		assert.Equal(t, admission.CodeEngineDisabled, *pd.Code)
	})

	t.Run("Sessions Full -> 503", func(t *testing.T) {
		// Limit 10, Active 10
		state := &MockAdmissionState{
			Tuners:     2,
			Sessions:   10,
			Transcodes: 0,
		}
		s := setupServer(state)

		reqBody := api.IntentRequest{
			Type:       model.IntentTypeStreamStart,
			ServiceRef: "1:0:1:0:0:0:0:0:0:0:http://example.com/stream",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v3/intents", bytes.NewReader(body))
		w := httptest.NewRecorder()

		s.handleV3Intents(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var pd ProblemDetails
		json.NewDecoder(resp.Body).Decode(&pd)
		require.NotNil(t, pd.Code)
		assert.Equal(t, admission.CodeSessionsFull, *pd.Code)
		assert.Equal(t, "5", resp.Header.Get("Retry-After")) // Check override
	})

	t.Run("No Tuners -> 503", func(t *testing.T) {
		state := &MockAdmissionState{
			Tuners:     0,
			Sessions:   0,
			Transcodes: 0,
		}
		s := setupServer(state)

		reqBody := api.IntentRequest{
			Type:       model.IntentTypeStreamStart,
			ServiceRef: "1:0:1:0:0:0:0:0:0:0:http://example.com/stream",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v3/intents", bytes.NewReader(body))
		w := httptest.NewRecorder()

		s.handleV3Intents(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var pd ProblemDetails
		json.NewDecoder(resp.Body).Decode(&pd)
		require.NotNil(t, pd.Code)
		assert.Equal(t, admission.CodeNoTuners, *pd.Code)
	})

	t.Run("State Unknown (Fail-Closed) -> 503", func(t *testing.T) {
		state := &MockAdmissionState{
			Err: assert.AnError, // Trigger -1 in collector
		}
		s := setupServer(state)

		reqBody := api.IntentRequest{
			Type:       model.IntentTypeStreamStart,
			ServiceRef: "1:0:1:0:0:0:0:0:0:0:http://example.com/stream",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v3/intents", bytes.NewReader(body))
		w := httptest.NewRecorder()

		s.handleV3Intents(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var pd ProblemDetails
		json.NewDecoder(resp.Body).Decode(&pd)
		require.NotNil(t, pd.Code)
		assert.Equal(t, admission.CodeStateUnknown, *pd.Code)
	})
}
