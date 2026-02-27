package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAdmissionState implements AdmissionState for testing
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
	const testServiceRef = "1:0:1:0:0:0:0:0:0:0:http://example.com/stream"

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
	memBus := bus.NewMemoryBus()
	store := &MockStoreForStreams{} // Use existing mock from streams_test

	setupServer := func(state *MockAdmissionState) *Server {
		s := NewServer(cfg, nil, func() {})
		s.v3Bus = memBus
		s.v3Store = store
		s.admissionState = state
		return s
	}

	t.Run("Engine Disabled -> 503", func(t *testing.T) {
		disabledCfg := cfg
		disabledCfg.Engine.Enabled = false
		s := setupServer(&MockAdmissionState{})
		s.cfg = disabledCfg
		s.admission = admission.NewController(disabledCfg)

		req := intentReqWithValidJWT(t, testServiceRef, "", "live")
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
		state := &MockAdmissionState{
			Tuners:     2,
			Sessions:   10,
			Transcodes: 0,
		}
		s := setupServer(state)

		req := intentReqWithValidJWT(t, testServiceRef, "", "live")
		w := httptest.NewRecorder()

		s.handleV3Intents(w, req)

		resp := w.Result()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		var pd ProblemDetails
		json.NewDecoder(resp.Body).Decode(&pd)
		require.NotNil(t, pd.Code)
		assert.Equal(t, admission.CodeSessionsFull, *pd.Code)
		assert.Equal(t, "5", resp.Header.Get("Retry-After"))
	})

	t.Run("No Tuners -> 503", func(t *testing.T) {
		state := &MockAdmissionState{
			Tuners:     0,
			Sessions:   0,
			Transcodes: 0,
		}
		s := setupServer(state)

		req := intentReqWithValidJWT(t, testServiceRef, "", "live")
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
			Err: assert.AnError,
		}
		s := setupServer(state)

		req := intentReqWithValidJWT(t, testServiceRef, "", "live")
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
