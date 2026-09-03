// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordLogin_Security_EnumerationAndRateLimiting(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "pwd_login_sec.sqlite")

	_, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	ctx := context.Background()

	// Create user "alice" with known password
	user := &identity.User{
		ID:          "usr_alice",
		Username:    "alice",
		DisplayName: "Alice",
		Role:        identity.RoleMember,
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, idSvc.Store().PutUser(ctx, user))
	hash, err := identity.HashPassword("CorrectPassword123!")
	require.NoError(t, err)
	require.NoError(t, idSvc.Store().PutAccountPassword(ctx, user.ID, hash, time.Now().UTC()))

	// 1. Wrong password for existing user: 401 Unauthorized
	wrongPayload, _ := json.Marshal(map[string]any{"username": "alice", "password": "WrongPassword!"})
	reqWrong := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(wrongPayload))
	reqWrong.Header.Set("Content-Type", "application/json")
	wWrong := httptest.NewRecorder()
	handler.ServeHTTP(wWrong, reqWrong)
	assert.Equal(t, http.StatusUnauthorized, wWrong.Code)

	var wrongBody map[string]any
	require.NoError(t, json.Unmarshal(wWrong.Body.Bytes(), &wrongBody))
	assert.Equal(t, "Invalid Credentials", wrongBody["title"])
	assert.Equal(t, "Invalid username or password", wrongBody["detail"])

	// 2. Non-existent username: exact same 401 response (no enumeration oracle)
	unknownPayload, _ := json.Marshal(map[string]any{"username": "non_existent_bob", "password": "AnyPassword!"})
	reqUnknown := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(unknownPayload))
	reqUnknown.Header.Set("Content-Type", "application/json")
	wUnknown := httptest.NewRecorder()
	handler.ServeHTTP(wUnknown, reqUnknown)
	assert.Equal(t, http.StatusUnauthorized, wUnknown.Code)

	var unknownBody map[string]any
	require.NoError(t, json.Unmarshal(wUnknown.Body.Bytes(), &unknownBody))
	assert.Equal(t, "Invalid Credentials", unknownBody["title"])
	assert.Equal(t, "Invalid username or password", unknownBody["detail"])

	// 3. Exceeding 10 requests from same client IP triggers HTTP 429 Too Many Requests
	for i := 0; i < 9; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(wrongPayload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// 11th request (2 above + 9 = 11 total from this IP)
	req11 := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(wrongPayload))
	req11.Header.Set("Content-Type", "application/json")
	w11 := httptest.NewRecorder()
	handler.ServeHTTP(w11, req11)

	assert.Equal(t, http.StatusTooManyRequests, w11.Code)
	assert.Equal(t, "60", w11.Header().Get("Retry-After"))
}
