package shadow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	v3api "github.com/ManuGH/xg2g/internal/v3/api"
)

func TestClient_Fire_Idempotency(t *testing.T) {
	var (
		mu           sync.Mutex
		lastIntent   v3api.IntentRequest
		lastHdrKey   string
		requestCount int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestCount++
		lastHdrKey = r.Header.Get("X-Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&lastIntent)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := Config{
		Enabled:   true,
		TargetURL: server.URL,
	}
	client := New(cfg)

	// Fire the intent
	// Note: Fire is async, but for test we want to wait or use execute directly to test logic.
	// We'll test `execute` via `Fire` by waiting a bit.
	client.Fire(context.Background(), "1:0:1:TEST:SERVICE", "hls_mobile", "192.168.1.50")

	// Wait for goroutine to complete
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if requestCount != 1 {
		t.Errorf("expected 1 request, got %d", requestCount)
	}

	if lastIntent.ServiceRef != "1:0:1:TEST:SERVICE" {
		t.Errorf("expected service ref match, got %s", lastIntent.ServiceRef)
	}

	// Verify IdempotencyKey field match
	if lastIntent.IdempotencyKey == "" {
		t.Error("expected IdempotencyKey field to be set")
	}
	if lastIntent.IdempotencyKey != lastHdrKey {
		t.Errorf("struct IdempotencyKey (%s) does not match header key (%s)", lastIntent.IdempotencyKey, lastHdrKey)
	}
}

func TestClient_Fire_TimeoutHelper(t *testing.T) {
	// Simple regression test to ensure Fire doesn't block
	// We point it to a "black hole" (or slow server) and ensure Fire returns instantly

	done := make(chan struct{})
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Signal request received but don't respond until much later
		close(done)
		time.Sleep(200 * time.Millisecond)
	}))
	defer slowServer.Close()

	cfg := Config{
		Enabled:   true,
		TargetURL: slowServer.URL,
	}
	client := New(cfg)

	start := time.Now()
	client.Fire(context.Background(), "ref", "prof", "ip")
	duration := time.Since(start)

	if duration > 10*time.Millisecond {
		t.Errorf("Fire() blocked for %v, expected instant return", duration)
	}
}
