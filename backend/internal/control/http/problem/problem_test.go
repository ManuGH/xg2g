package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/log"
)

func TestWritePreservesCanonicalProblemFields(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "http://example.test/v3/a%2Fb", nil)
	request = request.WithContext(log.ContextWithRequestID(request.Context(), "req-canonical"))
	response := httptest.NewRecorder()

	Write(
		response,
		request,
		http.StatusConflict,
		"sessions/conflict",
		"Conflict",
		"SESSION_CONFLICT",
		"session already exists",
		map[string]any{
			"status":         http.StatusOK,
			JSONKeyRequestID: "req-spoofed",
			"diagnostic":     "kept",
		},
	)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header().Get(HeaderRequestID); got != "req-canonical" {
		t.Fatalf("%s = %q", HeaderRequestID, got)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["type"]; got != "/problems/sessions/conflict" {
		t.Fatalf("type = %#v", got)
	}
	if got := body["status"]; got != float64(http.StatusConflict) {
		t.Fatalf("status body = %#v", got)
	}
	if got := body[JSONKeyRequestID]; got != "req-canonical" {
		t.Fatalf("%s body = %#v", JSONKeyRequestID, got)
	}
	if got := body["instance"]; got != "/v3/a%2Fb" {
		t.Fatalf("instance = %#v", got)
	}
	if got := body["diagnostic"]; got != "kept" {
		t.Fatalf("diagnostic = %#v", got)
	}
}

func TestWriteUsesHeaderThenFallbackRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headerID   string
		wantID     string
		withDetail bool
	}{
		{name: "existing response header", headerID: "req-header", wantID: "req-header", withDetail: true},
		{name: "missing request truth", wantID: FallbackRequestID},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			response.Header().Set(HeaderRequestID, tt.headerID)
			detail := ""
			if tt.withDetail {
				detail = "detail"
			}

			Write(response, nil, http.StatusBadRequest, "/problems/test", "Bad Request", "BAD_REQUEST", detail, nil)

			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got := body[JSONKeyRequestID]; got != tt.wantID {
				t.Fatalf("%s = %#v, want %q", JSONKeyRequestID, got, tt.wantID)
			}
			if _, ok := body["instance"]; ok {
				t.Fatal("nil request must not produce an instance")
			}
			_, hasDetail := body["detail"]
			if hasDetail != tt.withDetail {
				t.Fatalf("detail presence = %v, want %v", hasDetail, tt.withDetail)
			}
		})
	}
}
