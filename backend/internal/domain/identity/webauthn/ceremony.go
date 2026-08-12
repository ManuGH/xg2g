// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

var (
	ErrChallengeMismatch     = errors.New("webauthn challenge mismatch")
	ErrOriginMismatch        = errors.New("webauthn origin mismatch")
	ErrRPIDMismatch          = errors.New("webauthn rp id mismatch")
	ErrUserPresentRequired   = errors.New("user presence flag (UP) is required")
	ErrUserVerifiedRequired  = errors.New("user verification flag (UV) is required")
	ErrInvalidSignature      = errors.New("invalid webauthn signature")
	ErrSignCountDecreased    = errors.New("authenticator signature counter decreased on single-device security key")
	ErrSessionExpired        = errors.New("webauthn session has expired")
	ErrInvalidClientDataJSON = errors.New("invalid clientDataJSON")
)

// UserDescriptor describes the user identity during registration.
type UserDescriptor struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// CredentialState provides the current registered credential state for login verification.
type CredentialState struct {
	ID             string
	PublicKeyDER   []byte
	SignCount      uint32
	BackupEligible bool
	BackupState    bool
}

// AttestationResult contains the parsed and verified attestation output.
type AttestationResult struct {
	CredentialID    string
	PublicKeyDER    []byte
	AttestationType string
	AAGUID          string
	SignCount       uint32
	BackupEligible  bool
	BackupState     bool
	Transports      []string
}

// SessionData holds the in-flight challenge state between ceremony start and finish.
type SessionData struct {
	Challenge   string    `json:"challenge"`
	UserID      string    `json:"userId,omitempty"`
	Username    string    `json:"username,omitempty"`
	DisplayName string    `json:"displayName,omitempty"`
	IsBootstrap bool      `json:"isBootstrap,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (s SessionData) Valid(now time.Time) bool {
	return s.Challenge != "" && !now.After(s.ExpiresAt)
}

// CreationOptions matches WebAuthn PublicKeyCredentialCreationOptions.
type CreationOptions struct {
	RP struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"rp"`
	User struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"user"`
	Challenge              string            `json:"challenge"`
	PubKeyCredParams       []PubKeyCredParam `json:"pubKeyCredParams"`
	Timeout                int64             `json:"timeout"`
	AuthenticatorSelection struct {
		ResidentKey      string `json:"residentKey"`
		UserVerification string `json:"userVerification"`
	} `json:"authenticatorSelection"`
	Attestation string `json:"attestation"`
}

type PubKeyCredParam struct {
	Type string `json:"type"`
	Alg  int64  `json:"alg"`
}

// RequestOptions matches WebAuthn PublicKeyCredentialRequestOptions.
type RequestOptions struct {
	Challenge        string                 `json:"challenge"`
	Timeout          int64                  `json:"timeout"`
	RPID             string                 `json:"rpId"`
	UserVerification string                 `json:"userVerification"`
	AllowCredentials []CredentialDescriptor `json:"allowCredentials,omitempty"`
}

type CredentialDescriptor struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	Transports []string `json:"transports,omitempty"`
}

// ClientDataJSON is the parsed representation of the client data structure.
type ClientDataJSON struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin,omitempty"`
}

// AttestationResponse is the client response for registration.
type AttestationResponse struct {
	ClientDataJSON    string   `json:"clientDataJSON"`    // Base64URL
	AttestationObject string   `json:"attestationObject"` // Base64URL
	Transports        []string `json:"transports,omitempty"`
}

// AssertionResponse is the client response for login.
type AssertionResponse struct {
	CredentialID      string `json:"id"`
	ClientDataJSON    string `json:"clientDataJSON"`    // Base64URL
	AuthenticatorData string `json:"authenticatorData"` // Base64URL
	Signature         string `json:"signature"`         // Base64URL
	UserHandle        string `json:"userHandle,omitempty"`
}

// BeginRegistration initializes options for a new passkey registration.
func BeginRegistration(user UserDescriptor, rpID, rpName string, ttl time.Duration, now time.Time) (*CreationOptions, *SessionData, error) {
	challenge, err := newChallenge()
	if err != nil {
		return nil, nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	opts := &CreationOptions{
		Challenge:   challenge,
		Timeout:     60000,
		Attestation: "none",
	}
	opts.RP.ID = rpID
	opts.RP.Name = rpName
	opts.User.ID = base64.RawURLEncoding.EncodeToString([]byte(user.ID))
	opts.User.Name = user.Name
	opts.User.DisplayName = user.DisplayName
	opts.PubKeyCredParams = []PubKeyCredParam{
		{Type: "public-key", Alg: coseAlgES256},
		{Type: "public-key", Alg: coseAlgEdDSA},
		{Type: "public-key", Alg: coseAlgRS256},
	}
	opts.AuthenticatorSelection.ResidentKey = "preferred"
	opts.AuthenticatorSelection.UserVerification = "required"

	session := &SessionData{
		Challenge: challenge,
		UserID:    user.ID,
		ExpiresAt: now.Add(ttl),
	}
	return opts, session, nil
}

// FinishRegistration validates the client attestation and constructs a new AttestationResult.
func FinishRegistration(resp AttestationResponse, session SessionData, rpID, expectedOrigin string, now time.Time) (*AttestationResult, error) {
	if !session.Valid(now) {
		return nil, ErrSessionExpired
	}

	clientDataBytes, err := base64.RawURLEncoding.DecodeString(resp.ClientDataJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode clientDataJSON: %w", err)
	}

	var clientData ClientDataJSON
	if err := json.Unmarshal(clientDataBytes, &clientData); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidClientDataJSON, err)
	}

	if clientData.Type != "webauthn.create" {
		return nil, fmt.Errorf("expected clientData.type webauthn.create, got %s", clientData.Type)
	}
	if clientData.Challenge != session.Challenge {
		return nil, ErrChallengeMismatch
	}
	if !strings.EqualFold(clientData.Origin, expectedOrigin) {
		return nil, fmt.Errorf("%w: got %s, want %s", ErrOriginMismatch, clientData.Origin, expectedOrigin)
	}

	attObjBytes, err := base64.RawURLEncoding.DecodeString(resp.AttestationObject)
	if err != nil {
		return nil, fmt.Errorf("failed to decode attestationObject: %w", err)
	}

	attObj, err := DecodeCBOR(attObjBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to decode attestationObject CBOR: %w", err)
	}
	attMap, ok := attObj.(map[any]any)
	if !ok {
		return nil, errors.New("attestationObject is not a map")
	}

	authDataBytes, ok := attMap["authData"].([]byte)
	if !ok {
		return nil, errors.New("missing authData in attestationObject")
	}

	authData, err := ParseAuthenticatorData(authDataBytes, true)
	if err != nil {
		return nil, err
	}

	expectedRPIDHash := sha256.Sum256([]byte(rpID))
	if authData.RPIDHash != expectedRPIDHash {
		return nil, ErrRPIDMismatch
	}
	if !authData.UserPresent {
		return nil, ErrUserPresentRequired
	}
	if !authData.UserVerified {
		return nil, ErrUserVerifiedRequired
	}

	attFormat, _ := attMap["fmt"].(string)
	if attFormat == "" {
		attFormat = "none"
	}

	credIDBase64 := base64.RawURLEncoding.EncodeToString(authData.CredentialID)

	result := &AttestationResult{
		CredentialID:    credIDBase64,
		PublicKeyDER:    authData.PublicKeyDER,
		AttestationType: attFormat,
		AAGUID:          authData.AAGUID,
		SignCount:       authData.SignCount,
		BackupEligible:  authData.BackupEligible,
		BackupState:     authData.BackupState,
		Transports:      resp.Transports,
	}

	return result, nil
}

// BeginLogin initializes options for passkey authentication.
func BeginLogin(credentials []CredentialDescriptor, rpID string, ttl time.Duration, now time.Time) (*RequestOptions, *SessionData, error) {
	challenge, err := newChallenge()
	if err != nil {
		return nil, nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	opts := &RequestOptions{
		Challenge:        challenge,
		Timeout:          60000,
		RPID:             rpID,
		UserVerification: "required",
		AllowCredentials: credentials,
	}

	session := &SessionData{
		Challenge: challenge,
		ExpiresAt: now.Add(ttl),
	}
	return opts, session, nil
}

// FinishLogin validates the client assertion signature against the registered public key.
func FinishLogin(resp AssertionResponse, cred CredentialState, session SessionData, rpID, expectedOrigin string, now time.Time) (newSignCount uint32, isBackup bool, err error) {
	if !session.Valid(now) {
		return 0, false, ErrSessionExpired
	}

	clientDataBytes, err := base64.RawURLEncoding.DecodeString(resp.ClientDataJSON)
	if err != nil {
		return 0, false, fmt.Errorf("failed to decode clientDataJSON: %w", err)
	}

	var clientData ClientDataJSON
	if err := json.Unmarshal(clientDataBytes, &clientData); err != nil {
		return 0, false, fmt.Errorf("%w: %v", ErrInvalidClientDataJSON, err)
	}

	if clientData.Type != "webauthn.get" {
		return 0, false, fmt.Errorf("expected clientData.type webauthn.get, got %s", clientData.Type)
	}
	if clientData.Challenge != session.Challenge {
		return 0, false, ErrChallengeMismatch
	}
	if !strings.EqualFold(clientData.Origin, expectedOrigin) {
		return 0, false, fmt.Errorf("%w: got %s, want %s", ErrOriginMismatch, clientData.Origin, expectedOrigin)
	}

	authDataBytes, err := base64.RawURLEncoding.DecodeString(resp.AuthenticatorData)
	if err != nil {
		return 0, false, fmt.Errorf("failed to decode authenticatorData: %w", err)
	}

	authData, err := ParseAuthenticatorData(authDataBytes, false)
	if err != nil {
		return 0, false, err
	}

	expectedRPIDHash := sha256.Sum256([]byte(rpID))
	if authData.RPIDHash != expectedRPIDHash {
		return 0, false, ErrRPIDMismatch
	}
	if !authData.UserPresent {
		return 0, false, ErrUserPresentRequired
	}
	if !authData.UserVerified {
		return 0, false, ErrUserVerifiedRequired
	}

	// Sign count & Backup State handling:
	// If credential is Multi-Device (BE or BS set) or signCount is 0, do NOT reject non-monotone counters.
	isSyncedPasskey := cred.BackupEligible || cred.BackupState || authData.BackupEligible || authData.BackupState || authData.SignCount == 0
	if !isSyncedPasskey && cred.SignCount > 0 && authData.SignCount <= cred.SignCount {
		return 0, false, ErrSignCountDecreased
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(resp.Signature)
	if err != nil {
		return 0, false, fmt.Errorf("failed to decode signature: %w", err)
	}

	pub, err := x509.ParsePKIXPublicKey(cred.PublicKeyDER)
	if err != nil {
		return 0, false, fmt.Errorf("failed to parse stored public key: %w", err)
	}

	clientDataHash := sha256.Sum256(clientDataBytes)
	signedData := append(authDataBytes, clientDataHash[:]...)

	if err := verifySignature(pub, signedData, sigBytes); err != nil {
		return 0, false, err
	}

	return authData.SignCount, authData.BackupState, nil
}

func verifySignature(pub crypto.PublicKey, signedData, sigBytes []byte) error {
	digest := sha256.Sum256(signedData)

	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		// WebAuthn ECDSA signatures are ASN.1 DER encoded (r, s)
		var esig struct {
			R, S *big.Int
		}
		if _, err := asn1.Unmarshal(sigBytes, &esig); err == nil && esig.R != nil && esig.S != nil {
			if ecdsa.Verify(key, digest[:], esig.R, esig.S) {
				return nil
			}
		}
		// Raw (r || s) fallback (64 bytes)
		if len(sigBytes) == 64 {
			r := new(big.Int).SetBytes(sigBytes[:32])
			s := new(big.Int).SetBytes(sigBytes[32:])
			if ecdsa.Verify(key, digest[:], r, s) {
				return nil
			}
		}
		return ErrInvalidSignature

	case ed25519.PublicKey:
		if ed25519.Verify(key, signedData, sigBytes) {
			return nil
		}
		return ErrInvalidSignature

	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sigBytes); err == nil {
			return nil
		}
		return ErrInvalidSignature

	default:
		return fmt.Errorf("unsupported public key type %T", pub)
	}
}

func newChallenge() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
