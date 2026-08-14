// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	identitystore "github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/require"
)

func TestWireV3RuntimePreservesSeparatelyInjectedIdentityService(t *testing.T) {
	server := mustNewServer(t, config.AppConfig{}, config.NewManager(""))

	store, err := identitystore.OpenSQLite(filepath.Join(t.TempDir(), "identity.sqlite"), sqlite.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	identityService := identity.NewService(identity.Config{
		RPID:           "localhost",
		RPName:         "xg2g test",
		ExpectedOrigin: "http://localhost",
	}, store)
	server.SetIdentityService(identityService)
	requireIdentityConfigured(t, server)

	// bootstrap wires the identity service first, then applies the independently
	// composed runtime dependency set. That second DI pass must not erase identity.
	server.WireV3Runtime(v3.Dependencies{}, nil)

	requireIdentityConfigured(t, server)
}

func requireIdentityConfigured(t *testing.T, server *Server) {
	t.Helper()

	router, _, err := server.buildRouterWithBindings(ConfigVariantProdStatic)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v3/auth/status", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var status struct {
		Configured bool `json:"configured"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &status))
	require.True(t, status.Configured, "identity service was removed during v3 runtime wiring")
}
