package enigma2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStreamURL_StreamPortUsesOpenWebIFFirst(t *testing.T) {
	const sref = "1:0:19:83:6:85:C00000:0:0:0:"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/web/stream.m3u", r.URL.Path)
		assert.Equal(t, sref, r.URL.Query().Get("ref"))
		_, _ = w.Write([]byte("#EXTM3U\nhttp://127.0.0.1:17999/1:0:19:83:6:85:C00000:0:0:0:\n"))
	}))
	defer ts.Close()

	c := NewClientWithOptions(ts.URL, Options{
		Timeout:    time.Second,
		Username:   "root",
		Password:   "secret",
		StreamPort: 17999,
	})

	resolved, err := c.ResolveStreamURL(context.Background(), sref)
	require.NoError(t, err)
	assert.Equal(t, "http://root:secret@127.0.0.1:17999/1:0:19:83:6:85:C00000:0:0:0:", resolved)
}

func TestResolveStreamURL_StreamPortFallsBackToDirectURL(t *testing.T) {
	const sref = "1:0:19:83:6:85:C00000:0:0:0:"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/web/stream.m3u", r.URL.Path)
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer ts.Close()

	c := NewClientWithOptions(ts.URL, Options{
		Timeout:    time.Second,
		Username:   "root",
		Password:   "secret",
		StreamPort: 17999,
	})

	resolved, err := c.ResolveStreamURL(context.Background(), sref)
	require.NoError(t, err)
	parsed, err := url.Parse(ts.URL)
	require.NoError(t, err)
	assert.Equal(t, "http://root:secret@"+parsed.Hostname()+":17999/1:0:19:83:6:85:C00000:0:0:0:", resolved)
}
