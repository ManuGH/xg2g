// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHouseholdFullE2ESuite_MultiUserRolesInvitesAndEffectivePermissions(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "household_e2e.sqlite")

	_, handler, idSvc := setupTestV3ServerWithIdentity(t, dbPath)
	defer idSvc.Store().Close()

	ctx := context.Background()

	// 1. Initial Admin Bootstrap
	setupToken, err := idSvc.GenerateBootstrapToken(ctx)
	require.NoError(t, err)

	reqRegStart := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/start", nil)
	reqRegStart.Header.Set("X-Setup-Token", setupToken)
	reqRegStart.Header.Set("X-Forwarded-Proto", "https")
	wRegStart := httptest.NewRecorder()
	handler.ServeHTTP(wRegStart, reqRegStart)
	require.Equal(t, http.StatusOK, wRegStart.Code)

	var regStartOpts webauthn.CreationOptions
	require.NoError(t, json.Unmarshal(wRegStart.Body.Bytes(), &regStartOpts))

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	rawCredID := []byte("cred_papa_admin_123")
	clientDataBytes, _ := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": regStartOpts.Challenge, "origin": "https://localhost"})
	attObjBytes := buildE2EAttestationObject(&privKey.PublicKey, "localhost", rawCredID)

	finishRegPayload := map[string]any{
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataBytes),
			"attestationObject": base64.RawURLEncoding.EncodeToString(attObjBytes),
		},
		"nickname": "Papa Admin Key",
	}
	bodyRegFinish, _ := json.Marshal(finishRegPayload)

	reqRegFinish := httptest.NewRequest(http.MethodPost, "/api/v3/auth/passkey/register/finish", bytes.NewReader(bodyRegFinish))
	reqRegFinish.Header.Set("Content-Type", "application/json")
	reqRegFinish.Header.Set("X-Forwarded-Proto", "https")
	wRegFinish := httptest.NewRecorder()
	handler.ServeHTTP(wRegFinish, reqRegFinish)
	require.Equal(t, http.StatusOK, wRegFinish.Code)

	adminCookie := wRegFinish.Result().Cookies()[0]

	// 2. Admin creates invitation for Guest (Cousine Tina)
	invPayload := map[string]any{
		"role":        "guest",
		"displayName": "Cousine Tina",
	}
	invJSON, _ := json.Marshal(invPayload)

	reqInv := httptest.NewRequest(http.MethodPost, "/api/v3/auth/invitations", bytes.NewReader(invJSON))
	reqInv.Header.Set("Content-Type", "application/json")
	reqInv.AddCookie(adminCookie)
	wInv := httptest.NewRecorder()
	handler.ServeHTTP(wInv, reqInv)

	if wInv.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for /api/v3/auth/invitations, got %d. Body: %s", wInv.Code, wInv.Body.String())
	}
	var invRes map[string]any
	require.NoError(t, json.Unmarshal(wInv.Body.Bytes(), &invRes))
	inviteCode, ok := invRes["inviteCode"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, inviteCode)

	// 3. Guest redeems invitation with Argon2id Password
	redeemPayload := map[string]any{
		"inviteCode":  inviteCode,
		"username":    "tina",
		"displayName": "Cousine Tina",
		"password":    "guestPassword123!",
	}
	redeemJSON, _ := json.Marshal(redeemPayload)

	reqRedeem := httptest.NewRequest(http.MethodPost, "/api/v3/auth/invitations/redeem", bytes.NewReader(redeemJSON))
	reqRedeem.Header.Set("Content-Type", "application/json")
	wRedeem := httptest.NewRecorder()
	handler.ServeHTTP(wRedeem, reqRedeem)

	require.Equal(t, http.StatusOK, wRedeem.Code)
	guestCookie := wRedeem.Result().Cookies()[0]

	// 4. Atomic verification: repeat redeem must fail
	wRedeemRepeat := httptest.NewRecorder()
	reqRedeemRepeat := httptest.NewRequest(http.MethodPost, "/api/v3/auth/invitations/redeem", bytes.NewReader(redeemJSON))
	reqRedeemRepeat.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(wRedeemRepeat, reqRedeemRepeat)
	require.Equal(t, http.StatusBadRequest, wRedeemRepeat.Code)

	// 5. Password Login for Guest
	pwdLoginPayload := map[string]any{
		"username": "tina",
		"password": "guestPassword123!",
	}
	pwdJSON, _ := json.Marshal(pwdLoginPayload)

	reqPwdLogin := httptest.NewRequest(http.MethodPost, "/api/v3/auth/login/password", bytes.NewReader(pwdJSON))
	reqPwdLogin.Header.Set("Content-Type", "application/json")
	wPwdLogin := httptest.NewRecorder()
	handler.ServeHTTP(wPwdLogin, reqPwdLogin)

	require.Equal(t, http.StatusOK, wPwdLogin.Code)

	// 6. Admin creates Child Profile "Max 👦"
	profPayload := map[string]any{
		"name":            "Max 👦",
		"isChild":         true,
		"allowedBouquets": []string{"Kinder-TV"},
		"maturityLevel":   6,
		"exitPin":         "1234",
	}
	profJSON, _ := json.Marshal(profPayload)

	reqProf := httptest.NewRequest(http.MethodPost, "/api/v3/profiles", bytes.NewReader(profJSON))
	reqProf.Header.Set("Content-Type", "application/json")
	reqProf.AddCookie(adminCookie)
	wProf := httptest.NewRecorder()
	handler.ServeHTTP(wProf, reqProf)

	require.Equal(t, http.StatusOK, wProf.Code)
	var profRes map[string]any
	require.NoError(t, json.Unmarshal(wProf.Body.Bytes(), &profRes))
	profObj, ok := profRes["profile"].(map[string]any)
	require.True(t, ok)
	profID := profObj["id"].(string)

	// 7. Test EffectivePermissions Invariant: EffectivePermissions = AccountPermissions ∩ ProfilePolicy
	// A) Admin with Child Profile -> IsAdmin=true, but CanDVR=false (restricted by ProfilePolicy!)
	reqEffAdmin := httptest.NewRequest(http.MethodGet, "/api/v3/auth/effective-permissions?profile_id="+profID, nil)
	reqEffAdmin.AddCookie(adminCookie)
	wEffAdmin := httptest.NewRecorder()
	handler.ServeHTTP(wEffAdmin, reqEffAdmin)

	require.Equal(t, http.StatusOK, wEffAdmin.Code)
	var effAdmin map[string]any
	require.NoError(t, json.Unmarshal(wEffAdmin.Body.Bytes(), &effAdmin))
	assert.True(t, effAdmin["isAdmin"].(bool))
	assert.False(t, effAdmin["canDvr"].(bool), "Child profile policy MUST restrict DVR even for Admin")

	// B) Guest with Adult Profile -> IsAdmin=false, CanDVR=false (restricted by Account Role!)
	reqEffGuest := httptest.NewRequest(http.MethodGet, "/api/v3/auth/effective-permissions", nil)
	reqEffGuest.AddCookie(guestCookie)
	wEffGuest := httptest.NewRecorder()
	handler.ServeHTTP(wEffGuest, reqEffGuest)

	require.Equal(t, http.StatusOK, wEffGuest.Code)
	var effGuest map[string]any
	require.NoError(t, json.Unmarshal(wEffGuest.Body.Bytes(), &effGuest))
	assert.False(t, effGuest["isAdmin"].(bool))
	assert.True(t, effGuest["isGuest"].(bool))
	assert.False(t, effGuest["canDvr"].(bool), "Guest account role MUST NOT have DVR permissions")
}
