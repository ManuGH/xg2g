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

	// 1a. Boundary username: exactly 64 bytes (accepted by validation, rejected with 401 as invalid credentials)
	user64Payload, _ := json.Marshal(map[string]any{
		"username": string(bytes.Repeat([]byte("a"), 64)),
		"password": "validPassword123!",
	})
	reqUser64 := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(user64Payload))
	reqUser64.Header.Set("Content-Type", "application/json")
	wUser64 := httptest.NewRecorder()
	handler.ServeHTTP(wUser64, reqUser64)
	assert.Equal(t, http.StatusUnauthorized, wUser64.Code, "64-byte username is within acceptable boundary")

	// 1b. Oversized raw username: 65 bytes (rejected with 400 Bad Request)
	hugeUserPayload, _ := json.Marshal(map[string]any{
		"username": string(bytes.Repeat([]byte("a"), 65)),
		"password": "validPassword123!",
	})
	reqUser := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(hugeUserPayload))
	reqUser.Header.Set("Content-Type", "application/json")
	wUser := httptest.NewRecorder()
	handler.ServeHTTP(wUser, reqUser)
	assert.Equal(t, http.StatusBadRequest, wUser.Code, "65-byte raw username must be rejected")

	// 1c. Raw username with huge whitespace prefix exceeding 64 bytes total (e.g. 60 spaces + "admin" = 65 bytes)
	hugePrefixPayload, _ := json.Marshal(map[string]any{
		"username": string(bytes.Repeat([]byte(" "), 60)) + "admin",
		"password": "validPassword123!",
	})
	reqPrefix := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(hugePrefixPayload))
	reqPrefix.Header.Set("Content-Type", "application/json")
	wPrefix := httptest.NewRecorder()
	handler.ServeHTTP(wPrefix, reqPrefix)
	assert.Equal(t, http.StatusBadRequest, wPrefix.Code, "raw username with oversized whitespace prefix must be rejected before normalization")

	// 1d. Raw username with huge whitespace suffix exceeding 64 bytes total (e.g. "admin" + 60 spaces = 65 bytes)
	hugeSuffixPayload, _ := json.Marshal(map[string]any{
		"username": "admin" + string(bytes.Repeat([]byte(" "), 60)),
		"password": "validPassword123!",
	})
	reqSuffix := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(hugeSuffixPayload))
	reqSuffix.Header.Set("Content-Type", "application/json")
	wSuffix := httptest.NewRecorder()
	handler.ServeHTTP(wSuffix, reqSuffix)
	assert.Equal(t, http.StatusBadRequest, wSuffix.Code, "raw username with oversized whitespace suffix must be rejected before normalization")

	// 2a. Boundary password: exactly 128 bytes (accepted by validation, rejected with 401 as invalid credentials)
	pass128Payload, _ := json.Marshal(map[string]any{
		"username": "alice",
		"password": string(bytes.Repeat([]byte("p"), 128)),
	})
	reqPass128 := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(pass128Payload))
	reqPass128.Header.Set("Content-Type", "application/json")
	wPass128 := httptest.NewRecorder()
	handler.ServeHTTP(wPass128, reqPass128)
	assert.Equal(t, http.StatusUnauthorized, wPass128.Code, "128-byte password is within acceptable boundary")

	// 2b. Oversized password: 129 bytes (rejected with 400 Bad Request)
	hugePassPayload, _ := json.Marshal(map[string]any{
		"username": "alice",
		"password": string(bytes.Repeat([]byte("p"), 129)),
	})
	reqPass := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(hugePassPayload))
	reqPass.Header.Set("Content-Type", "application/json")
	wPass := httptest.NewRecorder()
	handler.ServeHTTP(wPass, reqPass)
	assert.Equal(t, http.StatusBadRequest, wPass.Code, "129-byte password must be rejected")

	// 3. Empty / Whitespace-only fields (rejected with 400 Bad Request)
	emptyPayload, _ := json.Marshal(map[string]any{"username": "   ", "password": "validPassword123!"})
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(emptyPayload))
	reqEmpty.Header.Set("Content-Type", "application/json")
	wEmpty := httptest.NewRecorder()
	handler.ServeHTTP(wEmpty, reqEmpty)
	assert.Equal(t, http.StatusBadRequest, wEmpty.Code, "whitespace-only username must be rejected")
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

func TestPasswordLogin_Security_UsernameCanonicalizationParity(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "pwd_login_canon.sqlite")

	_, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	// Create user "admin"
	adminUser := &identity.User{
		ID:          "usr_admin",
		Username:    "admin",
		DisplayName: "Admin",
		Role:        identity.RoleAdmin,
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, idSvc.Store().PutUser(context.Background(), adminUser))
	hash, err := identity.HashPassword("CorrectPassword123!")
	require.NoError(t, err)
	require.NoError(t, idSvc.Store().PutAccountPassword(context.Background(), adminUser.ID, hash, time.Now().UTC()))

	variants := []string{
		"admin",
		"Admin",
		"ADMIN",
		" admin",
		"admin ",
		"  AdMiN  ",
	}

	// Verify identity.NormalizeUsername produces identical canonical value for all variants
	for _, v := range variants {
		assert.Equal(t, "admin", identity.NormalizeUsername(v), "variant %q must normalize to canonical admin", v)
	}

	// Make 3 failed login attempts with 3 different casing/whitespace variants from different IPs
	for i := 0; i < 3; i++ {
		payload, _ := json.Marshal(map[string]any{"username": variants[i], "password": "WrongPassword!"})
		req := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(payload))
		req.RemoteAddr = "198.51.100." + string(rune('1'+i)) + ":1234"
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	}

	// The 4th attempt uses yet another variant from a 4th IP.
	// It MUST hit the account backoff bucket (>400ms delay) proving no rate-limit bypass via whitespace or casing.
	start := time.Now()
	payload4, _ := json.Marshal(map[string]any{"username": variants[3], "password": "WrongPassword!"})
	req4 := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(payload4))
	req4.RemoteAddr = "198.51.100.4:1234"
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, req4)
	duration := time.Since(start)

	assert.Equal(t, http.StatusUnauthorized, w4.Code)
	assert.True(t, duration >= 400*time.Millisecond, "all variants must hit the same account backoff bucket (elapsed: %v)", duration)
}
