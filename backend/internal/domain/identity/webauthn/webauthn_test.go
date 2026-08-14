// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package webauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebAuthn_BackupFlagsInvariant(t *testing.T) {
	// Invariant: BS=1 && BE=0 is illegal
	rpIDHash := sha256.Sum256([]byte("xg2g.home.matrixcentral.de"))

	makeAuthData := func(flags byte) []byte {
		buf := make([]byte, 37)
		copy(buf[:32], rpIDHash[:])
		buf[32] = flags
		binary.BigEndian.PutUint32(buf[33:37], 1)
		return buf
	}

	// 1. UP=1, BE=1, BS=1 (Valid synced passkey)
	validSynced := makeAuthData(flagUserPresent | flagBackupEligible | flagBackupState)
	ad, err := ParseAuthenticatorData(validSynced, false)
	require.NoError(t, err)
	assert.True(t, ad.BackupEligible)
	assert.True(t, ad.BackupState)

	// 2. UP=1, BE=1, BS=0 (Valid synced passkey, not yet backed up)
	validNotYetSynced := makeAuthData(flagUserPresent | flagBackupEligible)
	ad, err = ParseAuthenticatorData(validNotYetSynced, false)
	require.NoError(t, err)
	assert.True(t, ad.BackupEligible)
	assert.False(t, ad.BackupState)

	// 3. UP=1, BE=0, BS=0 (Valid single-device hardware key)
	validSingleDevice := makeAuthData(flagUserPresent)
	ad, err = ParseAuthenticatorData(validSingleDevice, false)
	require.NoError(t, err)
	assert.False(t, ad.BackupEligible)
	assert.False(t, ad.BackupState)

	// 4. UP=1, BE=0, BS=1 (INVALID: BS=1 without BE=1)
	invalidData := makeAuthData(flagUserPresent | flagBackupState)
	_, err = ParseAuthenticatorData(invalidData, false)
	require.ErrorIs(t, err, ErrInvalidBackupState)
}

func TestWebAuthn_FullRegistrationAndLoginFlow(t *testing.T) {
	rpID := "xg2g.home.matrixcentral.de"
	rpName := "xg2g Home Server"
	origin := "https://xg2g.home.matrixcentral.de"
	now := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)

	user := UserDescriptor{
		ID:          "usr_manuel",
		Name:        "manuel",
		DisplayName: "Manuel",
	}

	// ---------------- 1. Registration ----------------
	createOpts, regSession, err := BeginRegistration(user, rpID, rpName, 5*time.Minute, now)
	require.NoError(t, err)
	assert.NotEmpty(t, createOpts.Challenge)

	// Simulate Client Authenticator generating keypair
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	credID := []byte("cred_test_1234567890")
	aaguid, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f10")

	// Build raw COSE key
	// map: 1:2 (kty:EC2), 3:-7 (alg:ES256), -1:1 (crv:P-256), -2:x, -3:y
	coseMap := map[any]any{
		int64(1):  int64(coseKtyEC2),
		int64(3):  int64(coseAlgES256),
		int64(-1): int64(coseCrvP256),
		int64(-2): privKey.X.Bytes(),
		int64(-3): privKey.Y.Bytes(),
	}

	// Manual encode of cose map into CBOR
	coseBytes := encodeSimpleCBORMap(coseMap)

	// Build AuthData for Registration
	rpIDHash := sha256.Sum256([]byte(rpID))
	authDataReg := make([]byte, 37)
	copy(authDataReg[:32], rpIDHash[:])
	authDataReg[32] = flagUserPresent | flagUserVerified | flagAttestedCredential | flagBackupEligible | flagBackupState
	binary.BigEndian.PutUint32(authDataReg[33:37], 0)

	authDataReg = append(authDataReg, aaguid...)
	credIDLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(credIDLenBytes, uint16(len(credID)))
	authDataReg = append(authDataReg, credIDLenBytes...)
	authDataReg = append(authDataReg, credID...)
	authDataReg = append(authDataReg, coseBytes...)

	attObjMap := map[any]any{
		"fmt":      "none",
		"authData": authDataReg,
		"attStmt":  map[any]any{},
	}
	attObjBytes := encodeSimpleCBORMap(attObjMap)

	clientDataReg := ClientDataJSON{
		Type:      "webauthn.create",
		Challenge: regSession.Challenge,
		Origin:    origin,
	}
	clientDataRegJSON, _ := json.Marshal(clientDataReg)

	regResp := AttestationResponse{
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRegJSON),
		AttestationObject: base64.RawURLEncoding.EncodeToString(attObjBytes),
		Transports:        []string{"internal"},
	}

	cred, err := FinishRegistration(regResp, *regSession, rpID, origin, now)
	require.NoError(t, err)
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(credID), cred.CredentialID)
	assert.True(t, cred.BackupEligible)
	assert.True(t, cred.BackupState)

	// Test UV Missing in Registration Rejection
	authDataRegNoUV := make([]byte, len(authDataReg))
	copy(authDataRegNoUV, authDataReg)
	authDataRegNoUV[32] = flagUserPresent | flagAttestedCredential // UV missing!
	attObjMapNoUV := map[any]any{"fmt": "none", "authData": authDataRegNoUV, "attStmt": map[any]any{}}
	regRespNoUV := AttestationResponse{
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRegJSON),
		AttestationObject: base64.RawURLEncoding.EncodeToString(encodeSimpleCBORMap(attObjMapNoUV)),
		Transports:        []string{"internal"},
	}
	_, err = FinishRegistration(regRespNoUV, *regSession, rpID, origin, now)
	require.ErrorIs(t, err, ErrUserVerifiedRequired, "Registration without UV must be rejected")

	// ---------------- 2. Login (Assertion) ----------------
	credDesc := CredentialDescriptor{
		Type: "public-key",
		ID:   cred.CredentialID,
	}
	loginOpts, loginSession, err := BeginLogin([]CredentialDescriptor{credDesc}, rpID, 5*time.Minute, now)
	require.NoError(t, err)
	assert.NotEmpty(t, loginOpts.Challenge)
	assert.Equal(t, "required", loginOpts.UserVerification)

	clientDataLogin := ClientDataJSON{
		Type:      "webauthn.get",
		Challenge: loginSession.Challenge,
		Origin:    origin,
	}
	clientDataLoginJSON, _ := json.Marshal(clientDataLogin)
	clientDataHash := sha256.Sum256(clientDataLoginJSON)

	// Build AuthData for Login (Synced Passkey with count=5, UV=1)
	authDataLogin := make([]byte, 37)
	copy(authDataLogin[:32], rpIDHash[:])
	authDataLogin[32] = flagUserPresent | flagUserVerified | flagBackupEligible | flagBackupState
	binary.BigEndian.PutUint32(authDataLogin[33:37], 5)

	signedMessage := append(authDataLogin, clientDataHash[:]...)
	digest := sha256.Sum256(signedMessage)
	r, s, err := ecdsa.Sign(rand.Reader, privKey, digest[:])
	require.NoError(t, err)

	sigDER, err := asn1.Marshal(struct {
		R, S *big.Int
	}{r, s})
	require.NoError(t, err)

	loginResp := AssertionResponse{
		CredentialID:      cred.CredentialID,
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataLoginJSON),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(authDataLogin),
		Signature:         base64.RawURLEncoding.EncodeToString(sigDER),
	}

	credState := CredentialState{
		ID:             cred.CredentialID,
		PublicKeyDER:   cred.PublicKeyDER,
		SignCount:      cred.SignCount,
		BackupEligible: cred.BackupEligible,
		BackupState:    cred.BackupState,
	}

	newCount, isBackup, err := FinishLogin(loginResp, credState, *loginSession, rpID, origin, now)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), newCount)
	assert.True(t, isBackup)

	// Test UV Missing in Login Rejection
	authDataLoginNoUV := make([]byte, 37)
	copy(authDataLoginNoUV[:32], rpIDHash[:])
	authDataLoginNoUV[32] = flagUserPresent | flagBackupEligible | flagBackupState // UV=0!
	binary.BigEndian.PutUint32(authDataLoginNoUV[33:37], 6)
	signedNoUV := append(authDataLoginNoUV, clientDataHash[:]...)
	digestNoUV := sha256.Sum256(signedNoUV)
	rNoUV, sNoUV, _ := ecdsa.Sign(rand.Reader, privKey, digestNoUV[:])
	sigDERNoUV, _ := asn1.Marshal(struct{ R, S *big.Int }{rNoUV, sNoUV})
	loginRespNoUV := AssertionResponse{
		CredentialID:      cred.CredentialID,
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataLoginJSON),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(authDataLoginNoUV),
		Signature:         base64.RawURLEncoding.EncodeToString(sigDERNoUV),
	}
	_, _, err = FinishLogin(loginRespNoUV, credState, *loginSession, rpID, origin, now)
	require.ErrorIs(t, err, ErrUserVerifiedRequired, "Login without UV must be rejected")

	// ---------------- 3. Synced Passkey Non-Monotone Counter Tolerance ----------------
	// Next login from another synced device with counter=2 should NOT be rejected!
	authDataLogin2 := make([]byte, 37)
	copy(authDataLogin2[:32], rpIDHash[:])
	authDataLogin2[32] = flagUserPresent | flagUserVerified | flagBackupEligible | flagBackupState
	binary.BigEndian.PutUint32(authDataLogin2[33:37], 2)

	signedMessage2 := append(authDataLogin2, clientDataHash[:]...)
	digest2 := sha256.Sum256(signedMessage2)
	r2, s2, err := ecdsa.Sign(rand.Reader, privKey, digest2[:])
	require.NoError(t, err)
	sigDER2, err := asn1.Marshal(struct {
		R, S *big.Int
	}{r2, s2})
	require.NoError(t, err)

	loginResp2 := AssertionResponse{
		CredentialID:      cred.CredentialID,
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataLoginJSON),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(authDataLogin2),
		Signature:         base64.RawURLEncoding.EncodeToString(sigDER2),
	}

	newCount2, _, err := FinishLogin(loginResp2, credState, *loginSession, rpID, origin, now)
	require.NoError(t, err, "Synced passkeys must not be locked out on non-monotone counters")
	assert.Equal(t, uint32(2), newCount2)

	// ---------------- 4. Single-Device Security Key Counter Decrement Rejection ----------------
	singleDevCred := credState
	singleDevCred.BackupEligible = false
	singleDevCred.BackupState = false
	singleDevCred.SignCount = 100

	authDataSingleDevDecreased := make([]byte, 37)
	copy(authDataSingleDevDecreased[:32], rpIDHash[:])
	authDataSingleDevDecreased[32] = flagUserPresent | flagUserVerified // BE=0, BS=0, UV=1
	binary.BigEndian.PutUint32(authDataSingleDevDecreased[33:37], 50)   // Decreased counter!

	signedMessage3 := append(authDataSingleDevDecreased, clientDataHash[:]...)
	digest3 := sha256.Sum256(signedMessage3)
	r3, s3, _ := ecdsa.Sign(rand.Reader, privKey, digest3[:])
	sigDER3, _ := asn1.Marshal(struct {
		R, S *big.Int
	}{r3, s3})

	loginResp3 := AssertionResponse{
		CredentialID:      cred.CredentialID,
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataLoginJSON),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(authDataSingleDevDecreased),
		Signature:         base64.RawURLEncoding.EncodeToString(sigDER3),
	}

	_, _, err = FinishLogin(loginResp3, singleDevCred, *loginSession, rpID, origin, now)
	require.ErrorIs(t, err, ErrSignCountDecreased, "Single-device security key counter decrement must be rejected")
}

// Minimal CBOR encoder helper for test fixtures
func encodeSimpleCBORMap(m map[any]any) []byte {
	var buf []byte
	// map header
	buf = append(buf, 0xa0|byte(len(m)))
	for k, v := range m {
		buf = append(buf, encodeSimpleCBORItem(k)...)
		buf = append(buf, encodeSimpleCBORItem(v)...)
	}
	return buf
}

func encodeSimpleCBORItem(item any) []byte {
	switch v := item.(type) {
	case int64:
		if v >= 0 {
			if v < 24 {
				return []byte{byte(v)}
			}
			return []byte{24, byte(v)}
		}
		// Negative integer
		n := -1 - v
		if n < 24 {
			return []byte{0x20 | byte(n)}
		}
		return []byte{0x38, byte(n)}
	case string:
		var b []byte
		b = append(b, 0x60|byte(len(v)))
		b = append(b, []byte(v)...)
		return b
	case []byte:
		var b []byte
		if len(v) < 24 {
			b = append(b, 0x40|byte(len(v)))
		} else {
			b = append(b, 0x58, byte(len(v)))
		}
		b = append(b, v...)
		return b
	case map[any]any:
		return encodeSimpleCBORMap(v)
	default:
		return nil
	}
}
