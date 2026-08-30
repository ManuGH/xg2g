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

func TestAbout_CachingSingleflightAndLKGFallback(t *testing.T) {
	var requestCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/about" {
			count := atomic.AddInt64(&requestCount, 1)
			if count == 1 {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"info":{"model":"Vu+ Uno 4K","tuners":[{"name":"Tuner A","type":"DVB-S2"}]}}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"result":false,"message":"receiver error"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL)

	// 1. Initial fetch - hits HTTP endpoint
	about1, err := client.About(context.Background())
	require.NoError(t, err)
	require.NotNil(t, about1)
	assert.Equal(t, "Vu+ Uno 4K", about1.Info.Model)
	assert.Equal(t, int64(1), atomic.LoadInt64(&requestCount))

	// 2. Second fetch within 15s TTL - served directly from cache without HTTP request
	about2, err := client.About(context.Background())
	require.NoError(t, err)
	require.NotNil(t, about2)
	assert.Equal(t, "Vu+ Uno 4K", about2.Info.Model)
	assert.Equal(t, int64(1), atomic.LoadInt64(&requestCount), "Should be served from cache without HTTP call")

	// 3. Force cache expiration
	client.aboutCacheMu.Lock()
	client.aboutCacheAt = time.Now().Add(-20 * time.Second)
	client.aboutCacheMu.Unlock()

	// 4. Fetch when backend errors -> falls back to Last-Known-Good (LKG) cached about info
	about3, err := client.About(context.Background())
	require.NoError(t, err, "Should serve LKG cached about info on backend failure")
	require.NotNil(t, about3)
	assert.Equal(t, "Vu+ Uno 4K", about3.Info.Model)
	assert.Equal(t, int64(2), atomic.LoadInt64(&requestCount), "HTTP endpoint was called but failed")

	// 5. Invalidation
	client.InvalidateAboutCache()
	client.aboutCacheMu.RLock()
	assert.Nil(t, client.aboutCache)
	client.aboutCacheMu.RUnlock()
}

func TestGetStatusInfo_CachingSingleflightAndLKGFallback(t *testing.T) {
	var requestCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/statusinfo" {
			count := atomic.AddInt64(&requestCount, 1)
			if count == 1 {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"inStandby":"true","servicename":"Das Erste HD"}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"result":false,"message":"receiver error"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL)

	// 1. Initial fetch
	st1, err := client.GetStatusInfo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, st1)
	assert.Equal(t, "true", st1.InStandby)
	assert.Equal(t, int64(1), atomic.LoadInt64(&requestCount))

	// 2. Second fetch within 3s TTL
	st2, err := client.GetStatusInfo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, st2)
	assert.Equal(t, "true", st2.InStandby)
	assert.Equal(t, int64(1), atomic.LoadInt64(&requestCount), "Should be served from 3s cache without HTTP call")

	// 3. Invalidate
	client.InvalidateStatusCache()
	client.statusCacheMu.RLock()
	assert.Nil(t, client.statusCache)
	client.statusCacheMu.RUnlock()
}
