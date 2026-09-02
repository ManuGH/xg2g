package openwebif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// blockingServer answers path only after release is closed, so a caller's context
// can be cancelled while the upstream request is provably still in flight.
func blockingServer(t *testing.T, path, body string, started chan<- struct{}, release <-chan struct{}) *httptest.Server {
	t.Helper()
	var once atomic.Bool
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if once.CompareAndSwap(false, true) {
			close(started)
		}
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
}

// The singleflight body must not derive its timeout from the caller's context.
// The shared result belongs to every joined waiter, so the leader cancelling
// must not abort the upstream call. Cancelling the only caller mid-flight proves
// the detach: with the leader's ctx captured, the request dies with it.
func assertFlightSurvivesCallerCancel(t *testing.T, path, body string, call func(*Client, context.Context) error) {
	t.Helper()

	started := make(chan struct{})
	release := make(chan struct{})
	server := blockingServer(t, path, body, started, release)
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- call(client, ctx) }()

	<-started // the upstream request is now in flight
	cancel()  // the caller gives up
	close(release)

	select {
	case err := <-errCh:
		require.NoError(t, err, "cancelling the caller must not cancel the shared singleflight request")
	case <-time.After(10 * time.Second):
		t.Fatal("call did not return after the upstream request was released")
	}
}

func TestAbout_SingleflightDetachedFromCallerContext(t *testing.T) {
	assertFlightSurvivesCallerCancel(t, "/api/about", `{"info":{"model":"Vu+ Uno 4K"}}`,
		func(c *Client, ctx context.Context) error {
			_, err := c.About(ctx)
			return err
		})
}

func TestGetStatusInfo_SingleflightDetachedFromCallerContext(t *testing.T) {
	assertFlightSurvivesCallerCancel(t, "/api/statusinfo", `{"inStandby":"false"}`,
		func(c *Client, ctx context.Context) error {
			_, err := c.GetStatusInfo(ctx)
			return err
		})
}

// GetCurrent has no last-known-good fallback, so a poisoned flight surfaces
// directly as the caller's error.
func TestGetCurrent_SingleflightDetachedFromCallerContext(t *testing.T) {
	assertFlightSurvivesCallerCancel(t, "/api/getcurrent", `{"info":{"servicename":"Das Erste HD"}}`,
		func(c *Client, ctx context.Context) error {
			_, err := c.GetCurrent(ctx)
			return err
		})
}
