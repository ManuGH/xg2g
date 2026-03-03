package enigma2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadyChecker_HappyPath(t *testing.T) {
	// Sequence: Not Locked -> Wrong Ref -> Locked & Correct (x3)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 1 {
			_ = json.NewEncoder(w).Encode(CurrentInfo{}) // Empty/default
			return
		}
		switch r.URL.Path {
		case "/api/getcurrent":
			out := CurrentInfo{Result: true}
			out.Info.ServiceReference = "1:0:1:TEST:0:0:0:0:0:0:"
			if calls == 2 {
				out.Info.ServiceReference = "1:0:1:WRONG:0:0:0:0:0:0:" // Wrong Ref
			}
			_ = json.NewEncoder(w).Encode(out)
		case "/api/signal":
			out := Signal{Result: true, Locked: true}
			if calls <= 3 { // Until 3rd call set, stay unlocked
				out.Locked = false
			}
			_ = json.NewEncoder(w).Encode(out)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)
	rc := NewReadyChecker(client)
	rc.PollBase = 10 * time.Millisecond // fast test
	rc.DebounceN = 2

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := rc.WaitReady(ctx, "test-key", "1:0:1:TEST:0:0:0:0:0:0:")
	require.NoError(t, err)
}

func TestReadyChecker_Timeout(t *testing.T) {
	// Always upstream error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)
	rc := NewReadyChecker(client)
	rc.PollBase = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := rc.WaitReady(ctx, "test-key", "1:0:1:TEST:0:0:0:0:0:0:")
	assert.ErrorIs(t, err, ErrReadyTimeout)
}

func TestReadyChecker_WrongRef(t *testing.T) {
	// Locked but wrong Service Ref
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getcurrent" {
			ci := CurrentInfo{Result: true}
			ci.Info.ServiceReference = "1:0:1:OTHER:0:0:0:0:0:0:"
			_ = json.NewEncoder(w).Encode(ci)
		} else {
			_ = json.NewEncoder(w).Encode(Signal{Result: true, Locked: true})
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second)
	rc := NewReadyChecker(client)
	rc.PollBase = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := rc.WaitReady(ctx, "test-key", "1:0:1:RIGHT:0:0:0:0:0:0:")
	// Should eventualy timeout because it never becomes correct
	assert.ErrorIs(t, err, ErrReadyTimeout)
}
