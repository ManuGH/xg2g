package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/problem"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/bus"
)

type stubPreflightProvider struct {
	result     preflight.PreflightResult
	err        error
	calls      int
	lastSource preflight.SourceRef
}

func (s *stubPreflightProvider) Check(ctx context.Context, src preflight.SourceRef) (preflight.PreflightResult, error) {
	s.calls++
	s.lastSource = src
	return s.result, s.err
}

type spyBus struct {
	published int
}

func (s *spyBus) Publish(ctx context.Context, topic string, msg bus.Message) error {
	s.published++
	return nil
}

func (s *spyBus) Subscribe(ctx context.Context, topic string) (bus.Subscriber, error) {
	return nil, nil
}

func newPreflightIntentServer(t *testing.T, provider preflight.PreflightProvider) *Server {
	t.Helper()

	s, _ := newV3TestServer(t, t.TempDir())
	cfg := s.GetConfig()
	// Use localhost IP to avoid DNS resolution in tests
	cfg.Enigma2.BaseURL = "http://127.0.0.1:8001"
	// Enable outbound policy for preflight validation with CIDR allowlist (no DNS lookup)
	cfg.Network.Outbound.Enabled = true
	cfg.Network.Outbound.Allow.CIDRs = []string{"127.0.0.0/8"}
	cfg.Network.Outbound.Allow.Schemes = []string{"http", "https"}
	cfg.Network.Outbound.Allow.Ports = []int{80, 443, 8001}
	s.UpdateConfig(cfg, config.BuildSnapshot(cfg, config.DefaultEnv()))
	s.SetPreflightCheck(provider)
	return s
}

func newIntentRequest(t *testing.T, serviceRef string) *http.Request {
	t.Helper()

	// Build a request with a valid JWT that passes the security gate,
	// so that preflight tests actually test preflight logic, not auth.
	req := intentReqWithValidJWT(t, serviceRef, "", "live")
	return req.WithContext(log.ContextWithRequestID(req.Context(), "req-preflight-001"))
}

func TestIntentsPreflight_ProblemMapping(t *testing.T) {
	cases := []struct {
		name    string
		outcome preflight.PreflightOutcome
		status  int
	}{
		{"unreachable", preflight.PreflightUnreachable, http.StatusBadGateway},
		{"timeout", preflight.PreflightTimeout, http.StatusGatewayTimeout},
		{"unauthorized", preflight.PreflightUnauthorized, http.StatusUnauthorized},
		{"forbidden", preflight.PreflightForbidden, http.StatusForbidden},
		{"not_found", preflight.PreflightNotFound, http.StatusNotFound},
		{"bad_gateway", preflight.PreflightBadGateway, http.StatusBadGateway},
		{"internal", preflight.PreflightInternal, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			provider := &stubPreflightProvider{
				result: preflight.PreflightResult{Outcome: tc.outcome},
			}
			s := newPreflightIntentServer(t, provider)

			req := newIntentRequest(t, "1:0:1:ABC")
			rr := httptest.NewRecorder()
			s.handleV3Intents(rr, req)

			resp := rr.Result()
			require.Equal(t, tc.status, resp.StatusCode)
			require.Contains(t, resp.Header.Get("Content-Type"), "application/problem+json")

			var body map[string]any
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			require.Equal(t, float64(tc.status), body["status"])
			require.Equal(t, "/problems/preflight/"+string(tc.outcome), body["type"])
			require.NotEmpty(t, body["title"])
			require.NotEmpty(t, body["detail"])

			reqID, ok := body[problem.JSONKeyRequestID].(string)
			require.True(t, ok)
			require.NotEmpty(t, reqID)
		})
	}
}

func TestIntentsPreflight_NoBestEffort(t *testing.T) {
	provider := &stubPreflightProvider{
		result: preflight.PreflightResult{Outcome: preflight.PreflightUnreachable},
	}
	s := newPreflightIntentServer(t, provider)
	spy := &spyBus{}
	s.v3Bus = spy

	req := newIntentRequest(t, "1:0:1:ABC")
	rr := httptest.NewRecorder()
	s.handleV3Intents(rr, req)

	require.Equal(t, 0, spy.published)
}

func TestIntentsPreflight_Metrics(t *testing.T) {
	provider := &stubPreflightProvider{
		result: preflight.PreflightResult{Outcome: preflight.PreflightUnreachable},
	}
	s := newPreflightIntentServer(t, provider)

	before := getCounterValueByName(t, "xg2g_vod_preflight_fail_total", map[string]string{
		"reason": string(preflight.PreflightUnreachable),
	})

	req := newIntentRequest(t, "1:0:1:ABC")
	rr := httptest.NewRecorder()
	s.handleV3Intents(rr, req)

	after := getCounterValueByName(t, "xg2g_vod_preflight_fail_total", map[string]string{
		"reason": string(preflight.PreflightUnreachable),
	})

	require.Equal(t, before+1, after)
}

func TestIntentsPreflight_DetailNoSecrets(t *testing.T) {
	provider := &stubPreflightProvider{
		result: preflight.PreflightResult{Outcome: preflight.PreflightUnauthorized},
	}
	s := newPreflightIntentServer(t, provider)

	req := newIntentRequest(t, "1:0:1:token=secret")
	rr := httptest.NewRecorder()
	s.handleV3Intents(rr, req)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	detail, _ := body["detail"].(string)
	require.NotContains(t, detail, "token")
	require.NotContains(t, detail, "Bearer")
	require.NotContains(t, detail, "Authorization")
}

func getCounterValueByName(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()

	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.Metric {
			if labelsMatch(m, labels) {
				if m.Counter == nil {
					t.Fatalf("metric %s is not a counter", name)
				}
				return m.Counter.GetValue()
			}
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func labelsMatch(m *dto.Metric, labels map[string]string) bool {
	if len(labels) == 0 {
		return len(m.Label) == 0
	}

	labelMap := make(map[string]string, len(m.Label))
	for _, pair := range m.Label {
		labelMap[pair.GetName()] = pair.GetValue()
	}

	for key, value := range labels {
		if labelMap[key] != value {
			return false
		}
	}
	return true
}
