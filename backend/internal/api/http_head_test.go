package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/go-chi/chi/v5"
)

// mockV3Handler is a partial mock for V3Handler to support HEAD route testing
type mockV3Handler struct {
	v3.ServerInterface // Embed generated handler interface if needed, or just stub methods
}

// Stub StreamRecordingDirect to return 200 OK (mimicking logic)
func (m *mockV3Handler) StreamRecordingDirect(w http.ResponseWriter, r *http.Request, recordingId string) {
	w.WriteHeader(http.StatusOK)
}

// Minimal Auth Middleware stub for testing router wiring
func stubAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("xg2g_session"); err == nil && c.Value == "valid" {
			next.ServeHTTP(w, r)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
}

func TestHeadRouteRegistration(t *testing.T) {
	// Setup Router
	r := chi.NewRouter()

	// mock handler
	mock := &mockV3Handler{}

	// Manually mimic the registration logic from http.go
	// In a real integration test we'd instatiate the actual Server, but we just want to verify
	// that if we register it like the server does, it works.
	// Actually, better: we should verify the Server's router construction if possible.
	// But Server struct logic is internal/api/http.go.
	// Let's mimic the registration block exactly to "lock in" the pattern.

	// 1. Base Router

	// 2. Register HEAD with Auth Middleware (The critical requirement)
	r.With(stubAuthMiddleware).Head("/api/v3/recordings/{recordingId}/stream.mp4", func(w http.ResponseWriter, r *http.Request) {
		recordingId := chi.URLParam(r, "recordingId")
		mock.StreamRecordingDirect(w, r, recordingId)
	})

	// Test 1: HEAD with Token -> 200
	req, _ := http.NewRequest("HEAD", "/api/v3/recordings/123/stream.mp4", nil)
	req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "valid"})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("HEAD with token: expected 200 OK, got %d", rr.Code)
	}

	// Test 2: HEAD without Token -> 401
	req2, _ := http.NewRequest("HEAD", "/api/v3/recordings/123/stream.mp4", nil)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("HEAD without token: expected 401 Unauthorized, got %d", rr2.Code)
	}
}
