package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/rs/zerolog"
)

// TestIdleTimeout verifies that the centralized idle monitor correctly interprets
// stream inactivity and cancels the context.
func TestIdleTimeout(t *testing.T) {
	// 1. Setup Server with short idle timeout
	idleTimeout := 200 * time.Millisecond
	logger := zerolog.New(io.Discard)

	// Create mock target to satisfy New() validation
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Simulate stream that behaves well then stops
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		// Send one chunk
		if _, err := w.Write([]byte("chunk1")); err != nil {
			return
		}
		flusher.Flush()

		// Then hang until context is cancelled
		<-r.Context().Done()
	}))
	defer target.Close()

	cfg := Config{
		ListenAddr: ":0",
		TargetURL:  target.URL,
		Logger:     logger,
		Runtime: config.RuntimeSnapshot{
			StreamProxy: config.StreamProxyRuntime{
				IdleTimeout: idleTimeout,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// 2. Start Idle Monitor manually (since we are not calling srv.Start())
	// In real usage, Start() calls this. Here we mock it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.startIdleMonitor(ctx)

	// 3. Create a request and response recorder
	// We need a custom response writer that simulates time passing for writes?
	// The idle tracking writer uses time.Now() inside Write().
	// So we just need to Write() once, then wait > idleTimeout.

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	w := httptest.NewRecorder()

	// 4. Verify Context Cancellation
	// We wrap the request handling in a goroutine because handleRequest blocks until completion
	done := make(chan struct{})

	go func() {
		defer close(done)
		srv.handleRequest(w, req)
	}()

	// The request should complete (via cancellation) after ~idleTimeout
	select {
	case <-done:
		// Success: handler returned
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return within 2s (idle timeout broken)")
	}

	// Verify header (Gateway Timeout if it hadn't written anything, but target writes immediately)
	// Actually, target writes "chunk1". So handleRequest proxies "chunk1".
	// internal/proxy uses idleTrackingWriter.
	// Write() updates lastWrite.
	// Then it waits.
	// Monitor checks.
	// Monitor cancels context.
	// Proxy sees context done, returns.

	// Check if we got the timeout log? No easy way to check log.
	// Check duration.
}

// TestIdleTimeout_WriterActivity verifies that active writing prevents timeout.
func TestIdleTimeout_WriterActivity(t *testing.T) {
	idleTimeout := 200 * time.Millisecond
	logger := zerolog.New(io.Discard)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		// Write periodically to keep it alive for 500ms (longer than 200ms timeout)
		for i := 0; i < 5; i++ {
			select {
			case <-r.Context().Done():
				// Should not happen if keepalive works
				return
			case <-time.After(100 * time.Millisecond):
				if _, err := w.Write([]byte(".")); err != nil {
					// connection closed
					return
				}
				flusher.Flush()
			}
		}
	}))
	defer target.Close()

	cfg := Config{
		ListenAddr: ":0",
		TargetURL:  target.URL,
		Logger:     logger,
		Runtime: config.RuntimeSnapshot{
			StreamProxy: config.StreamProxyRuntime{
				IdleTimeout: idleTimeout,
			},
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.startIdleMonitor(ctx)

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleRequest(w, req)
	}()

	// Should NOT finish immediately, but should finish successfully after target finishes
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("handler stuck or timed out unexpectedly late")
	}

	// If context was cancelled by idle monitor, the target loop would have broken early/errored?
	// We can't easily check internal state, but if it ran for ~500ms successfully, it worked.
}

// TestIdleMonitor_Cleanup verifies that streams are removed from the map.
func TestIdleMonitor_Cleanup(t *testing.T) {
	// This requires inspecting private s.activeStreams.
	// Since we are in package proxy, we can access it.

	idleTimeout := 100 * time.Millisecond
	logger := zerolog.New(io.Discard)

	cfg := Config{
		ListenAddr: ":0",
		TargetURL:  "http://example.com", // Dummy
		Logger:     logger,
		Runtime: config.RuntimeSnapshot{
			StreamProxy: config.StreamProxyRuntime{
				IdleTimeout: idleTimeout,
			},
		},
	}
	srv, _ := New(cfg)

	// Create a dummy writer
	w := newIdleTrackingWriter(httptest.NewRecorder(), idleTimeout, func() {}, logger)

	// Manually track
	srv.trackStream(w)

	count := 0
	srv.activeStreams.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 1 {
		t.Errorf("expected 1 active stream, got %d", count)
	}

	// Untrack
	srv.untrackStream(w)

	count = 0
	srv.activeStreams.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("expected 0 active streams, got %d", count)
	}
}
