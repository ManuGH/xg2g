package scan

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOriginalProbeURL_ResolvesPlaylistTarget(t *testing.T) {
	t.Helper()

	target := "http://receiver.example:17999/1:0:1:ABC"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "#EXTM3U\n%s\n", target)
	}))
	defer srv.Close()

	resolved, resolvedPlaylist, err := resolveOriginalProbeURL(context.Background(), srv.URL+"/stream.m3u")
	require.NoError(t, err)
	require.True(t, resolvedPlaylist)
	require.Equal(t, target, resolved)
}

func TestResolveOriginalProbeURL_PassthroughForDirectURLs(t *testing.T) {
	t.Helper()

	url := "http://receiver.example:8001/1:0:1:ABC"
	resolved, resolvedPlaylist, err := resolveOriginalProbeURL(context.Background(), url)
	require.NoError(t, err)
	require.False(t, resolvedPlaylist)
	require.Equal(t, url, resolved)
}

func TestHasAttemptedProbeURL(t *testing.T) {
	t.Helper()

	attempted := map[string]struct{}{
		normalizeProbeURL("http://root:secret@receiver.example:17999/1:0:1:ABC"): {},
	}

	require.True(t, hasAttemptedProbeURL(attempted, "http://receiver.example:17999/1:0:1:ABC"))
	require.True(t, hasAttemptedProbeURL(attempted, "http://root:secret@receiver.example:17999/1:0:1:ABC"))
	require.False(t, hasAttemptedProbeURL(attempted, "http://receiver.example:8001/1:0:1:ABC"))
	require.False(t, hasAttemptedProbeURL(attempted, ""))
}
