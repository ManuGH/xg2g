package v3

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
)

func TestIntentHandlerForType(t *testing.T) {
	if handler, ok := intentHandlerForType(model.IntentTypeStreamStart); !ok || handler == nil {
		t.Fatalf("expected start intent handler, got ok=%v", ok)
	}

	if handler, ok := intentHandlerForType(model.IntentTypeStreamStop); !ok || handler == nil {
		t.Fatalf("expected stop intent handler, got ok=%v", ok)
	}

	if handler, ok := intentHandlerForType(model.IntentType("unknown.intent")); ok || handler != nil {
		t.Fatalf("expected unknown intent to have no handler, got ok=%v handler_nil=%v", ok, handler == nil)
	}
}

func TestIntentRoute_EndToEnd(t *testing.T) {
	s := newPlaybackModeIntentServer(t)
	s.cfg.APIToken = "test-token"
	s.cfg.APITokenScopes = []string{string(ScopeV3Write)}

	body, err := json.Marshal(v3api.IntentRequest{
		Type:       model.IntentTypeStreamStart,
		ServiceRef: "1:0:1:1337:42:99:0:0:0:0:",
		Params:     map[string]string{"profile": "high"},
	})
	if err != nil {
		t.Fatalf("marshal intent request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, V3BaseURL+"/intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()

	NewRouter(s, RouterOptions{BaseURL: V3BaseURL}).ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp v3api.IntentResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode intent response: %v", err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("expected status accepted, got %q", resp.Status)
	}
	if resp.SessionID == "" {
		t.Fatal("expected sessionId in response")
	}
}
