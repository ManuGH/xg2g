package v3

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockScan struct {
	calls int32
	ch    chan struct{}
}

func (m *mockScan) RunBackground() bool {
	atomic.AddInt32(&m.calls, 1)
	if m.ch != nil {
		select {
		case m.ch <- struct{}{}:
		default:
		}
	}
	return true
}

func (m *mockScan) GetCapability(serviceRef string) (scan.Capability, bool) {
	return scan.Capability{}, false // Stub - not used in refresh tests
}

func TestContract_SystemRefresh(t *testing.T) {
	cfg := config.AppConfig{}
	snap := config.Snapshot{}

	t.Run("21_RefreshUnavailable_NoScanner", func(t *testing.T) {
		s := &Server{cfg: cfg, snap: snap, v3Scan: nil}
		req := httptest.NewRequest("POST", "/api/v3/system/refresh", nil)
		w := httptest.NewRecorder()

		s.PostSystemRefresh(w, req)

		assertProblemDetails(t, w.Result(), 503, "system/unavailable")
	})

	t.Run("22_RefreshAccepted_TriggersBackground", func(t *testing.T) {
		ms := &mockScan{ch: make(chan struct{}, 1)}
		s := &Server{cfg: cfg, snap: snap, v3Scan: ms}

		req := httptest.NewRequest("POST", "/api/v3/system/refresh", nil)
		w := httptest.NewRecorder()

		s.PostSystemRefresh(w, req)

		resp := w.Result()
		require.Equal(t, 202, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

		// Deterministic: ensure goroutine called RunBackground (bounded wait)
		select {
		case <-ms.ch:
			// ok
		case <-time.After(250 * time.Millisecond):
			t.Fatalf("expected refresh to trigger RunBackground")
		}

		assert.Equal(t, int32(1), atomic.LoadInt32(&ms.calls))
	})
}
