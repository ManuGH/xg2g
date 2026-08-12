// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package v3_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasskeyHTTPContract_ExactEndpointAndPayloadVerification(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity_contract.sqlite")

	_, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	// 1. Generate Setup Token for Bootstrap
	setupToken, err := idSvc.GenerateBootstrapToken(context.Background())
	require.NoError(t, err)

	// 2. Test PasskeyRegisterStart (POST /api/v3/auth/passkey/register/start with X-Setup-Token)
	reqStart := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/start", nil)
	reqStart.Header.Set("X-Setup-Token", setupToken)
	reqStart.Header.Set("X-Forwarded-Proto", "https")
	wStart := httptest.NewRecorder()
	handler.ServeHTTP(wStart, reqStart)

	require.Equal(t, http.StatusOK, wStart.Code, "Register start with X-Setup-Token must return 200 OK")
	var startData map[string]any
	require.NoError(t, json.Unmarshal(wStart.Body.Bytes(), &startData))
	assert.Contains(t, startData, "challenge", "Register start response must contain challenge")
	assert.Contains(t, startData, "rp", "Register start response must contain rp")

	// 3. Test RecoveryLogin Endpoint Route (POST /api/v3/auth/recovery - NOT /recovery/login)
	// Missing params should return 400 Bad Request (proving endpoint exists and parses body)
	reqRecoveryBad := httptest.NewRequest(http.MethodPost, "/api/v3/auth/recovery", nil)
	wRecoveryBad := httptest.NewRecorder()
	handler.ServeHTTP(wRecoveryBad, reqRecoveryBad)
	require.Equal(t, http.StatusBadRequest, wRecoveryBad.Code, "POST /api/v3/auth/recovery must return 400 for empty body")

	// 4. Test RevokeOtherSessions Method (POST /api/v3/auth/sessions/revoke-others - NOT DELETE)
	// Unauthenticated request should return 401 Unauthorized (proving endpoint exists and requires auth)
	reqRevokePost := httptest.NewRequest(http.MethodPost, "/api/v3/auth/sessions/revoke-others", nil)
	wRevokePost := httptest.NewRecorder()
	handler.ServeHTTP(wRevokePost, reqRevokePost)
	require.Equal(t, http.StatusUnauthorized, wRevokePost.Code, "POST /api/v3/auth/sessions/revoke-others must return 401 unauth")

	// DELETE /api/v3/auth/sessions/revoke-others must return 405 Method Not Allowed or 404 (proving DELETE is wrong!)
	reqRevokeDelete := httptest.NewRequest(http.MethodDelete, "/api/v3/auth/sessions/revoke-others", nil)
	wRevokeDelete := httptest.NewRecorder()
	handler.ServeHTTP(wRevokeDelete, reqRevokeDelete)
	assert.NotEqual(t, http.StatusUnauthorized, wRevokeDelete.Code, "DELETE method on /auth/sessions/revoke-others must not match POST route handler")

	// 5. Test AcknowledgeRecovery Route (POST /api/v3/auth/bootstrap/acknowledge-recovery - NOT /bootstrap/commit)
	reqAckBad := httptest.NewRequest(http.MethodPost, "/api/v3/auth/bootstrap/acknowledge-recovery", nil)
	wAckBad := httptest.NewRecorder()
	handler.ServeHTTP(wAckBad, reqAckBad)
	require.Equal(t, http.StatusUnauthorized, wAckBad.Code, "POST /api/v3/auth/bootstrap/acknowledge-recovery must return 401 unauth")

	// 6. Test Passkeys List Array Format (GET /api/v3/auth/passkeys)
	reqPasskeysGet := httptest.NewRequest(http.MethodGet, "/api/v3/auth/passkeys", nil)
	wPasskeysGet := httptest.NewRecorder()
	handler.ServeHTTP(wPasskeysGet, reqPasskeysGet)
	require.Equal(t, http.StatusUnauthorized, wPasskeysGet.Code, "GET /api/v3/auth/passkeys must return 401 unauth")
}
