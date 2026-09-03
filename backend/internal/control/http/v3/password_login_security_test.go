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

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
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

func TestPasswordLogin_Security_InputSizeLimits(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "pwd_login_input.sqlite")

	_, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	// 1. Oversized username (>64 bytes)
	hugeUserPayload, _ := json.Marshal(map[string]any{
		"username": bytes.Repeat([]byte("a"), 65),
		"password": "validPassword123!",
	})
	reqUser := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(hugeUserPayload))
	reqUser.Header.Set("Content-Type", "application/json")
	wUser := httptest.NewRecorder()
	handler.ServeHTTP(wUser, reqUser)
	assert.Equal(t, http.StatusBadRequest, wUser.Code)

	// 2. Oversized password (>128 bytes)
	hugePassPayload, _ := json.Marshal(map[string]any{
		"username": "alice",
		"password": bytes.Repeat([]byte("p"), 129),
	})
	reqPass := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(hugePassPayload))
	reqPass.Header.Set("Content-Type", "application/json")
	wPass := httptest.NewRecorder()
	handler.ServeHTTP(wPass, reqPass)
	assert.Equal(t, http.StatusBadRequest, wPass.Code)

	// 3. Empty fields
	emptyPayload, _ := json.Marshal(map[string]any{"username": "   ", "password": ""})
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(emptyPayload))
	reqEmpty.Header.Set("Content-Type", "application/json")
	wEmpty := httptest.NewRecorder()
	handler.ServeHTTP(wEmpty, reqEmpty)
	assert.Equal(t, http.StatusBadRequest, wEmpty.Code)
}

func TestPasswordLogin_Security_UntrustedXFFSpoofingBlocked(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "pwd_login_xff.sqlite")

	_, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	wrongPayload, _ := json.Marshal(map[string]any{"username": "alice", "password": "wrong"})

	// An attacker sends 10 requests from remote addr 1.2.3.4:1234 but sets fake X-Forwarded-For headers
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(wrongPayload))
		req.RemoteAddr = "1.2.3.4:1234"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "8.8.8.8, 9.9.9.9") // Spoofed headers
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// 11th request from the same RemoteAddr with another spoofed header must be rate limited!
	req11 := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(wrongPayload))
	req11.RemoteAddr = "1.2.3.4:1234"
	req11.Header.Set("Content-Type", "application/json")
	req11.Header.Set("X-Forwarded-For", "10.10.10.10")
	w11 := httptest.NewRecorder()
	handler.ServeHTTP(w11, req11)

	assert.Equal(t, http.StatusTooManyRequests, w11.Code, "untrusted XFF must NOT bypass per-IP rate limiter")
}

func TestPasswordLogin_Security_ContextAwareBackoff(t *testing.T) {
	limiter := v3.NewPasswordLoginLimiter()
	// Record 3 failures for user "charlie" to trigger backoff delay
	for i := 0; i < 3; i++ {
		limiter.RecordFailure("10.0.0.1", "charlie")
	}

	// Create context with very short timeout (10ms)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := limiter.CheckAllowed(ctx, "10.0.0.1", "charlie")
	duration := time.Since(start)

	// Invariant: must abort on context cancellation and not wait out the full 500ms backoff
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, duration < 100*time.Millisecond, "must abort immediately on context cancellation")
}

func TestPasswordLogin_Security_AccountBackoffAcrossIPs(t *testing.T) {
	limiter := v3.NewPasswordLoginLimiter()

	// 3 failures across 3 different IPs for the same account "victim_user"
	limiter.RecordFailure("192.168.1.1", "victim_user")
	limiter.RecordFailure("192.168.1.2", "victim_user")
	limiter.RecordFailure("192.168.1.3", "victim_user")

	// 4th request from a 4th IP "192.168.1.4" experiences the backoff delay (capped, never hard lockout)
	start := time.Now()
	err := limiter.CheckAllowed(context.Background(), "192.168.1.4", "victim_user")
	duration := time.Since(start)

	require.NoError(t, err)
	assert.True(t, duration >= 400*time.Millisecond, "account-wide backoff must apply across multiple attacker IPs")

	// Upon successful login, the account backoff is reset
	limiter.RecordSuccess("192.168.1.4", "victim_user")

	startPostSuccess := time.Now()
	errPostSuccess := limiter.CheckAllowed(context.Background(), "192.168.1.5", "victim_user")
	durationPostSuccess := time.Since(startPostSuccess)

	require.NoError(t, errPostSuccess)
	assert.True(t, durationPostSuccess < 50*time.Millisecond, "success resets account backoff state")
}
