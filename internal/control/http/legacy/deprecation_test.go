// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package legacy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type runtimeStub struct {
	cfg config.AppConfig
}

func (r runtimeStub) CurrentConfig() config.AppConfig {
	return r.cfg
}

func (runtimeStub) PlaylistFilename() string {
	return "playlist.m3u"
}

func (runtimeStub) ResolveDataFilePath(string) (string, error) {
	return "", nil
}

func (runtimeStub) HDHomeRunServer() *hdhr.Server {
	return nil
}

func (runtimeStub) PiconSemaphore() chan struct{} {
	return nil
}

func TestLegacyRoutesSetDeprecationHeaders(t *testing.T) {
	router := chi.NewRouter()
	lanGuard, err := middleware.NewLANGuard(middleware.LANGuardConfig{})
	require.NoError(t, err)

	RegisterRoutes(router, runtimeStub{
		cfg: config.AppConfig{
			DataDir: t.TempDir(),
		},
	}, lanGuard)

	req := httptest.NewRequest(http.MethodGet, "/xmltv.xml", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, "true", rr.Header().Get("Deprecation"))
	links := rr.Header().Values("Link")
	require.NotEmpty(t, links)
	combined := strings.Join(links, ",")
	require.Contains(t, combined, `rel="deprecation"`)
	require.Contains(t, combined, `rel="sunset"`)
}
