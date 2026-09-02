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

// firstOKThenFail serves one successful body, then fails every later request.
func firstOKThenFail(t *testing.T, path, body string, count *int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if atomic.AddInt64(count, 1) == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"result":false,"message":"receiver error"}`))
	}))
}

// An unreachable receiver must stop being reported as observed once the cached
// reading ages past the LKG window. Before the bound, one successful fetch made
// every later failure serve that cache forever.
func TestAbout_LKGFallbackExpiresPastBound(t *testing.T) {
	var requests int64
	server := firstOKThenFail(t, "/api/about", `{"info":{"model":"Vu+ Uno 4K"}}`, &requests)
	defer server.Close()

	client := New(server.URL)

	_, err := client.About(context.Background())
	require.NoError(t, err)

	// Still inside the LKG window: the stale reading is served on failure.
	client.aboutCacheMu.Lock()
	client.aboutCacheAt = time.Now().Add(-(maxAboutLKGAge - time.Second))
	client.aboutCacheMu.Unlock()

	about, err := client.About(context.Background())
	require.NoError(t, err, "within maxAboutLKGAge the cached reading is still served")
	require.NotNil(t, about)
	assert.Equal(t, "Vu+ Uno 4K", about.Info.Model)

	// Past the window: the caller gets the error back instead of stale data.
	client.aboutCacheMu.Lock()
	client.aboutCacheAt = time.Now().Add(-(maxAboutLKGAge + time.Second))
	client.aboutCacheMu.Unlock()

	about, err = client.About(context.Background())
	require.Error(t, err, "past maxAboutLKGAge the failure must surface, not stale about info")
	assert.Nil(t, about)
}

func TestGetStatusInfo_LKGFallbackExpiresPastBound(t *testing.T) {
	var requests int64
	server := firstOKThenFail(t, "/api/statusinfo", `{"inStandby":"true","servicename":"Das Erste HD"}`, &requests)
	defer server.Close()

	client := New(server.URL)

	_, err := client.GetStatusInfo(context.Background())
	require.NoError(t, err)

	client.statusCacheMu.Lock()
	client.statusCacheAt = time.Now().Add(-(maxStatusLKGAge - time.Second))
	client.statusCacheMu.Unlock()

	st, err := client.GetStatusInfo(context.Background())
	require.NoError(t, err, "within maxStatusLKGAge the cached reading is still served")
	require.NotNil(t, st)
	assert.Equal(t, "true", st.InStandby)

	client.statusCacheMu.Lock()
	client.statusCacheAt = time.Now().Add(-(maxStatusLKGAge + time.Second))
	client.statusCacheMu.Unlock()

	st, err = client.GetStatusInfo(context.Background())
	require.Error(t, err, "past maxStatusLKGAge the failure must surface, not stale standby state")
	assert.Nil(t, st)
}

func TestGetTimers_LKGFallbackExpiresPastBound(t *testing.T) {
	var requests int64
	server := firstOKThenFail(t, "/api/timerlist", `{"timers":[{"servicereference":"1:0:1:1","name":"Tatort","begin":1,"end":2}]}`, &requests)
	defer server.Close()

	client := New(server.URL)

	timers, err := client.GetTimers(context.Background())
	require.NoError(t, err)
	require.Len(t, timers, 1)

	client.timerCacheMu.Lock()
	client.timerCacheAt = time.Now().Add(-(maxTimerLKGAge - time.Second))
	client.timerCacheMu.Unlock()

	timers, err = client.GetTimers(context.Background())
	require.NoError(t, err, "within maxTimerLKGAge the cached list is still served")
	require.Len(t, timers, 1)

	client.timerCacheMu.Lock()
	client.timerCacheAt = time.Now().Add(-(maxTimerLKGAge + time.Second))
	client.timerCacheMu.Unlock()

	timers, err = client.GetTimers(context.Background())
	require.Error(t, err, "past maxTimerLKGAge the failure must surface, not a stale recording list")
	assert.Nil(t, timers)
}
