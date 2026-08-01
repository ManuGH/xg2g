package openwebif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTimers_CachingSingleflightAndLKGFallback(t *testing.T) {
	var requestCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/timerlist" {
			count := atomic.AddInt64(&requestCount, 1)
			if count == 1 {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"result":true,"timers":[{"sRef":"1:0:1:1:1:1:1:0:0:0:","name":"Test Timer 1"}]}`))
				return
			}
			// Fail subsequent requests to test LKG fallback
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"result":false,"message":"receiver busy"}`))
			return
		}
		if r.URL.Path == "/api/timeradd" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":true,"message":"Timer added"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL)

	// 1. Initial fetch - hits HTTP endpoint
	timers1, err := client.GetTimers(context.Background())
	require.NoError(t, err)
	require.Len(t, timers1, 1)
	assert.Equal(t, "Test Timer 1", timers1[0].Name)
	assert.Equal(t, int64(1), atomic.LoadInt64(&requestCount))

	// 2. Second fetch within 5s TTL - served directly from cache without HTTP request
	timers2, err := client.GetTimers(context.Background())
	require.NoError(t, err)
	require.Len(t, timers2, 1)
	assert.Equal(t, "Test Timer 1", timers2[0].Name)
	assert.Equal(t, int64(1), atomic.LoadInt64(&requestCount), "Should be served from 5s cache without HTTP call")

	// 3. Force cache expiration (simulate time passing > 5s)
	client.timerCacheMu.Lock()
	client.timerCacheAt = time.Now().Add(-10 * time.Second)
	client.timerCacheMu.Unlock()

	// 4. Fetch when backend errors -> falls back to Last-Known-Good (LKG) cached timers
	timers3, err := client.GetTimers(context.Background())
	require.NoError(t, err, "Should serve LKG cached timers on backend failure")
	require.Len(t, timers3, 1)
	assert.Equal(t, "Test Timer 1", timers3[0].Name)
	assert.Equal(t, int64(2), atomic.LoadInt64(&requestCount), "HTTP endpoint was called but failed")

	// 5. Mutation (AddTimer) invalidates cache
	err = client.AddTimer(context.Background(), "1:0:1:1:1:1:1:0:0:0:", 1000, 2000, "Timer 2", "Desc")
	require.NoError(t, err)

	client.timerCacheMu.RLock()
	assert.Nil(t, client.timerCache, "AddTimer must invalidate timer cache")
	client.timerCacheMu.RUnlock()
}
