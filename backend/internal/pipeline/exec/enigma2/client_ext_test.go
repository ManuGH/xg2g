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

func TestResolveStreamURL_UseWebIFStreams(t *testing.T) {
	const sref = "1:0:19:83:6:85:C00000:0:0:0:"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/web/stream.m3u", r.URL.Path)
		_, _ = w.Write([]byte("#EXTM3U\nhttp://10.0.0.1:8001/1:0:19:83:6:85:C00000:0:0:0:\n"))
	}))
	defer ts.Close()

	c := NewClientWithOptions(ts.URL, Options{
		UseWebIFStreams: true,
	})

	resolved, err := c.ResolveStreamURL(context.Background(), sref)
	require.NoError(t, err)
	assert.Equal(t, "http://10.0.0.1:8001/1:0:19:83:6:85:C00000:0:0:0:", resolved)
}

func TestResolveStreamURL_DirectBypass(t *testing.T) {
	c := NewClientWithOptions("http://dummy", Options{})
	resolved, err := c.ResolveStreamURL(context.Background(), "http://192.168.1.10:8001/1:0:1:2:3:4:5:0:0:0:")
	require.NoError(t, err)
	assert.Equal(t, "http://192.168.1.10:8001/1:0:1:2:3:4:5:0:0:0:", resolved)
}

func TestNormalizeStreamURL(t *testing.T) {
	tests := []struct {
		rawURL   string
		sref     string
		expected string
	}{
		{"", "1:0:1:2:3:4:5", ""},
		{"http://host:8001", "", "http://host:8001"},
		{"http://host:8001/1:0:1:2:3:4:5:0:0:0:", "1:0:1:2", "http://host:8001/1:0:1:2:3:4:5:0:0:0:"},
		{"http://host:8001", "1:0:1:2", "http://host:8001/1:0:1:2"},
		{"http://host:8001/", "1:0:1:2", "http://host:8001/1:0:1:2"},
	}
	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeStreamURL(tt.rawURL, tt.sref))
		})
	}
}

func TestResolveStreamLine(t *testing.T) {
	tests := []struct {
		baseURL  string
		line     string
		sref     string
		expected string
		ok       bool
	}{
		{"http://base", "", "1:0", "", false},
		{"http://base", "garbage", "1:0", "", false},
		{"http://base", "http://host:8001/stream", "1:0", "http://host:8001/stream", true},
		{"http://base", "/stream", "1:0", "http://base/stream", true},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			res, ok := resolveStreamLine(tt.baseURL, tt.line, tt.sref)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.expected, res)
		})
	}
}
