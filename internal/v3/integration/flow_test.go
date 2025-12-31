//go:build v3
// +build v3

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/api"
	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/lease"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
	"github.com/ManuGH/xg2g/internal/v3/worker"
)

// TestV3Flow verifies the end-to-end flow: Intent -> Bus -> Worker -> Store.
func TestV3Flow(t *testing.T) {
	// Arrange: Infrastructure
	memStore := store.NewMemoryStore()
	memBus := bus.NewMemoryBus()
	defer memStore.Close()

	// Arrange: API
	intentHandler := api.IntentHandler{
		Store:          memStore,
		Bus:            memBus,
		IdempotencyTTL: 1 * time.Minute,
		TunerSlots:     []int{0},
	}

	// Arrange: Worker
	orch := worker.Orchestrator{
		Store:          memStore,
		Bus:            memBus,
		LeaseTTL:       2 * time.Second,
		HeartbeatEvery: 500 * time.Millisecond,
		TunerSlots:     []int{0},
	}

	// Start Worker
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := orch.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("orchestrator startup failed: %v", err)
		}
	}()
	// Ensure the orchestrator has subscribed before publishing intents.
	time.Sleep(50 * time.Millisecond)

	t.Run("Happy Path: Intent -> Ready", func(t *testing.T) {
		// Act: Send Intent
		reqBody := `{"serviceRef": "1:0:1:...:TestService", "profile": "web_opt"}`
		req := httptest.NewRequest("POST", "/api/v3/intents", strings.NewReader(reqBody))
		w := httptest.NewRecorder()
		intentHandler.ServeHTTP(w, req)

		// Assert: 202 Accepted
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", w.Code)
		}

		var resp api.IntentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.SessionID == "" {
			t.Error("expected sessionID")
		}
		if resp.Status != "accepted" {
			t.Errorf("expected status accepted, got %s", resp.Status)
		}

		// Assert: Wait for Worker to pick it up and mark READY
		// Because memory bus is "best effort / async", we poll the store.
		deadline := time.Now().Add(2 * time.Second)
		success := false
		for time.Now().Before(deadline) {
			sess, err := memStore.GetSession(ctx, resp.SessionID)
			if err == nil && sess != nil && sess.State == model.SessionReady {
				success = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !success {
			// dump state for debugging
			sess, _ := memStore.GetSession(ctx, resp.SessionID)
			t.Errorf("timeout waiting for session READY. Current: %v", sess)
		}
	})

	t.Run("Idempotency: Same Key -> Same Session", func(t *testing.T) {
		key := "test-idem-key-123"
		reqBody := `{"serviceRef": "1:0:1:Duplicate", "profile": "web_opt", "idempotencyKey": "` + key + `"}`

		// First Call
		w1 := httptest.NewRecorder()
		r1 := httptest.NewRequest("POST", "/api/v3/intents", strings.NewReader(reqBody))
		// We can set Header too
		// r1.Header.Set("Idempotency-Key", key)
		intentHandler.ServeHTTP(w1, r1)

		var resp1 api.IntentResponse
		_ = json.NewDecoder(w1.Body).Decode(&resp1)

		// Second Call
		w2 := httptest.NewRecorder()
		r2 := httptest.NewRequest("POST", "/api/v3/intents", strings.NewReader(reqBody))
		intentHandler.ServeHTTP(w2, r2)

		var resp2 api.IntentResponse
		_ = json.NewDecoder(w2.Body).Decode(&resp2)

		if resp1.SessionID != resp2.SessionID {
			t.Errorf("expected same sessionID for same idempotency key (%s vs %s)", resp1.SessionID, resp2.SessionID)
		}
	})

	t.Run("Lease Contention: Admission Rejects", func(t *testing.T) {
		svc := "1:0:1:Contentious"

		// Pre-acquire tuner lease to force admission busy.
		_, ok, err := memStore.TryAcquireLease(ctx, lease.LeaseKeyTunerSlot(0), "manual-test-owner", 5*time.Second)
		if err != nil || !ok {
			t.Fatalf("failed to pre-acquire lease: %v", err)
		}
		defer memStore.ReleaseLease(ctx, lease.LeaseKeyTunerSlot(0), "manual-test-owner")

		reqBody := `{"serviceRef": "` + svc + `", "profile": "web_opt"}`
		w := httptest.NewRecorder()
		intentHandler.ServeHTTP(w, httptest.NewRequest("POST", "/api/v3/intents", strings.NewReader(reqBody)))

		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", w.Code)
		}

		sessions, err := memStore.ListSessions(ctx)
		if err != nil {
			t.Fatalf("list sessions failed: %v", err)
		}
		for _, sess := range sessions {
			if sess.ServiceRef == svc {
				t.Fatalf("unexpected session created during admission rejection: %v", sess)
			}
		}
	})

	// Cleanup
	cancel()
	wg.Wait()
}
