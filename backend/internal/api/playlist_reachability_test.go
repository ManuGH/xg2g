// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer_PlaylistReachabilityAndLeakPrevention verifies that the production
// api.Server.Handler() correctly serves masked playlists via /playlist.m3u and
// /playlist_public.m3u, blocks raw canary IPTV leaks, fails closed when the public
// export is absent, and rejects XMLTV/EPG requests (Option B fail-closed).
func TestServer_PlaylistReachabilityAndLeakPrevention(t *testing.T) {
	dataDir := t.TempDir()

	canaryToken := "CANARY_TOKEN_SECRET_XYZ_98765"
	canaryURL := "http://provider.invalid/live/auth_user/stream.ts"
	rawServiceRef := "4097:0:1:DEAD:BEEF:CAFE:0:0:0:0:http%3a//provider.invalid/live/auth_user/stream.ts"
	opaqueID := "iptv_01j7canaryopaqueid1234"
	dvbServiceRef := "1:0:19:AAAA:BBBB:CCCC:0:0:0:0:"

	// 1. Internal raw playlist.m3u with 0600 permissions
	rawContent := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"" + rawServiceRef + "\" tvg-name=\"Canary Stream\"," + canaryToken + "\n" +
		canaryURL + "\n" +
		"#EXTINF:-1 tvg-id=\"" + dvbServiceRef + "\" tvg-name=\"Demo DVB\",Demo DVB\n" +
		"http://receiver.invalid:8001/" + dvbServiceRef + "\n"

	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "playlist.m3u"), []byte(rawContent), 0600))

	// 2. Public masked playlist_public.m3u with 0644 permissions
	publicContent := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"" + opaqueID + "\" tvg-name=\"Canary Stream\" tvg-logo=\"/logos/" + opaqueID + ".png\",Canary Stream\n" +
		"/api/v3/stream/live/" + opaqueID + "\n" +
		"#EXTINF:-1 tvg-id=\"" + dvbServiceRef + "\" tvg-name=\"Demo DVB\",Demo DVB\n" +
		"http://receiver.invalid:8001/" + dvbServiceRef + "\n"

	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "playlist_public.m3u"), []byte(publicContent), 0644))

	// 3. Raw xmltv.xml and epg.xml containing canary secrets
	rawXMLTV := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<tv>\n" +
		"  <channel id=\"" + rawServiceRef + "\">\n" +
		"    <display-name>" + canaryToken + "</display-name>\n" +
		"  </channel>\n</tv>\n"
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "xmltv.xml"), []byte(rawXMLTV), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "epg.xml"), []byte(rawXMLTV), 0600))

	// 4. Construct production server and obtain s.Handler()
	s := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	handler := s.Handler()
	require.NotNil(t, handler)

	canaryMarkers := []string{
		canaryToken,
		"provider.invalid",
		"auth_user",
		rawServiceRef,
	}

	// 5. Test GET and HEAD for /playlist.m3u and /playlist_public.m3u via s.Handler()
	for _, reqPath := range []string{"/playlist.m3u", "/playlist_public.m3u"} {
		t.Run("GET "+reqPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, reqPath, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code, "s.Handler() must return 200 OK for %s", reqPath)
			assert.Contains(t, rec.Header().Get("Content-Type"), "audio/x-mpegurl")
			assert.NotEmpty(t, rec.Header().Get("ETag"))

			body := rec.Body.String()
			for _, marker := range canaryMarkers {
				assert.NotContains(t, body, marker, "endpoint %s leaked canary marker %q", reqPath, marker)
			}
			assert.Contains(t, body, opaqueID, "endpoint %s must contain masked opaque ID", reqPath)
			assert.Contains(t, body, "/api/v3/stream/live/"+opaqueID)
			assert.Equal(t, publicContent, body, "endpoint %s must serve the complete masked export", reqPath)
		})

		t.Run("HEAD "+reqPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodHead, reqPath, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code, "s.Handler() must return 200 OK for HEAD %s", reqPath)
			assert.Contains(t, rec.Header().Get("Content-Type"), "audio/x-mpegurl")
			assert.Empty(t, rec.Body.String(), "HEAD response body must be empty")
		})
	}

	// 6. Test XMLTV / EPG blocking on s.Handler() (Option B fail-closed)
	for _, blockedPath := range []string{"/xmltv.xml", "/epg.xml"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+blockedPath, func(t *testing.T) {
				req := httptest.NewRequest(method, blockedPath, nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				assert.Equal(t, http.StatusNotFound, rec.Code, "endpoint %s must return 404", blockedPath)
				body := rec.Body.String()
				for _, marker := range canaryMarkers {
					assert.NotContains(t, body, marker, "blocked endpoint %s leaked canary marker %q", blockedPath, marker)
				}
			})
		}
	}
}

// TestServer_PlaylistFailClosedWhenPublicExportAbsent verifies that when
// playlist_public.m3u is absent, requests for /playlist.m3u return 404 and
// never fall back to serving the internal playlist.m3u containing raw secrets.
func TestServer_PlaylistFailClosedWhenPublicExportAbsent(t *testing.T) {
	dataDir := t.TempDir()

	canarySecret := "SUPER_SECRET_CANARY_RAW_KEY_123"
	rawContent := "#EXTM3U\n#EXTINF:-1," + canarySecret + "\nhttp://secret.stream/live\n"
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "playlist.m3u"), []byte(rawContent), 0600))
	// Notice: playlist_public.m3u is intentionally NOT created!

	s := mustNewServer(t, config.AppConfig{
		DataDir: dataDir,
	}, config.NewManager(""))
	handler := s.Handler()
	require.NotNil(t, handler)

	for _, reqPath := range []string{"/playlist.m3u", "/playlist_public.m3u"} {
		req := httptest.NewRequest(http.MethodGet, reqPath, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code, "must return 404 when public export is absent for %s", reqPath)
		assert.NotContains(t, rec.Body.String(), canarySecret, "must never leak internal playlist content")
	}
}
