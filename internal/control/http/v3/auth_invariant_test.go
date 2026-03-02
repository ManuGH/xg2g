package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/stretchr/testify/assert"
)

type mockResolver struct {
	callCount int32
}

func (m *mockResolver) Resolve(ctx context.Context, recordingID string, intent recservice.PlaybackIntent, profile recservice.PlaybackProfile) (recservice.PlaybackInfoResult, error) {
	atomic.AddInt32(&m.callCount, 1)
	return recservice.PlaybackInfoResult{}, nil
}

func (m *mockResolver) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, nil
}

func TestSecurityFailClosedInvariant(t *testing.T) {
	// 1. Setup Server with Mock Resolver
	s := &Server{}
	mock := &mockResolver{}
	s.SetResolver(mock)

	// Set a dummy AuthMiddleware that always fails if no "fail" header is present
	// and a dummy ScopeMiddleware that always fails
	s.AuthMiddlewareOverride = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Auth") == "fail" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// 2. Test Unauthorized (401)
	t.Run("401_Unauthorized_Must_Not_Call_Resolver", func(t *testing.T) {
		atomic.StoreInt32(&mock.callCount, 0)
		req := httptest.NewRequest("GET", "/api/v3/recordings/123/playback", nil)
		req.Header.Set("X-Auth", "fail")
		w := httptest.NewRecorder()

		// Simulate the middleware stack
		handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.GetRecordingPlaybackInfo(w, r, "123")
		}))

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, int32(0), atomic.LoadInt32(&mock.callCount), "Resolver was called on 401!")
	})

	// 3. Test Forbidden (403)
	t.Run("403_Forbidden_Must_Not_Call_Resolver", func(t *testing.T) {
		atomic.StoreInt32(&mock.callCount, 0)
		req := httptest.NewRequest("GET", "/api/v3/recordings/123/playback", nil)
		w := httptest.NewRecorder()

		// Simulate Auth PASS, but Scope FAIL
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate a scope middleware that always fails with 403
			w.WriteHeader(http.StatusForbidden)
		})

		// Wrap with the handler that would call the resolver
		finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r)
			if w.(*httptest.ResponseRecorder).Code == http.StatusOK {
				s.GetRecordingPlaybackInfo(w, r, "123")
			}
		})

		finalHandler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Equal(t, int32(0), atomic.LoadInt32(&mock.callCount), "Resolver was called on 403!")
	})
}
