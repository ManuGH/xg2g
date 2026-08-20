// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// DeviceGrantStart handles POST /api/v3/auth/device/grant/start
func (s *Server) DeviceGrantStart(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req DeviceGrantStartRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	opts, err := svc.BeginPasskeyLogin(r.Context(), stringValue(req.Username))
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin device passkey assertion")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/login_failed", "Assertion Failed", problemcode.CodeInvalidInput, "Failed to begin passkey assertion", nil)
		return
	}

	writeJSON(w, http.StatusOK, webAuthnRequestOptions(opts))
}

// DeviceGrantFinish handles POST /api/v3/auth/device/grant/finish
func (s *Server) DeviceGrantFinish(w http.ResponseWriter, r *http.Request, params DeviceGrantFinishParams) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	// Presence is enforced by the generated wrapper now that the contract
	// declares the header. What is left here is proving it — and its key is the
	// one the grant binds to, which is why the body carries no key of its own.
	validator := s.getDPoPValidator()
	now := time.Now().UTC()
	proofClaims, err := validator.ValidateProof(r, params.DPoP, "", now)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("invalid DPoP proof during device grant finish")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/invalid_dpop", "Invalid DPoP Proof", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	var req DeviceGrantFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid JSON Body", problemcode.CodeInvalidInput, "Invalid JSON request body", nil)
		return
	}

	// 1. Verify User Identity via Passkey WITHOUT setting browser cookies
	user, _, err := svc.VerifyPasskeyAssertion(r.Context(), domainAssertion(req.Assertion))
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("passkey assertion failed during android device grant")
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/login_failed", "Authentication Failed", problemcode.CodeUnauthorized, "Invalid passkey assertion", nil)
		return
	}

	deviceName := stringValue(req.DeviceName)
	if deviceName == "" {
		deviceName = "Android Device"
	}
	platform := stringValue(req.Platform)
	if platform == "" {
		platform = "android"
	}

	// 2. Issue DPoP-bound Device Grant & Access Token
	grantRes, err := svc.IssueDeviceGrant(r.Context(), user.ID, deviceName, platform, proofClaims.Header.JWK, stringValue(req.Scopes), identity.GrantTypePasskeyEnrollment)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to issue device grant")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "auth/grant_failed", "Grant Failed", problemcode.CodeInternalServerError, "Failed to issue device grant", nil)
		return
	}

	log.FromContext(r.Context()).Info().
		Str("user_id", user.ID).
		Str("device_id", grantRes.DeviceID).
		Str("jkt", proofClaims.JWKThumbprint).
		Msg("issued DPoP-bound device grant for android")

	// Set mandatory RFC 9449 / OAuth2 token headers
	w.Header().Set("Cache-Control", "no-store, no-cache, private")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, deviceGrantResponse(grantRes))
}

// DeviceRefresh handles POST /api/v3/auth/device/refresh
func (s *Server) DeviceRefresh(w http.ResponseWriter, r *http.Request, params DeviceRefreshParams) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	// Presence is enforced by the generated wrapper now that the contract
	// declares the header. What is left here is proving it.
	validator := s.getDPoPValidator()
	now := time.Now().UTC()
	proofClaims, err := validator.ValidateProof(r, params.DPoP, "", now)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("invalid DPoP proof during device refresh")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/invalid_dpop", "Invalid DPoP Proof", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	var req DeviceRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Refresh Token", problemcode.CodeInvalidInput, "Missing or invalid refresh_token payload", nil)
		return
	}

	grantRes, err := svc.RotateDeviceRefreshToken(r.Context(), req.RefreshToken, proofClaims.JWKThumbprint)
	if err != nil {
		if errors.Is(err, identity.ErrRefreshTokenReplay) {
			log.FromContext(r.Context()).Error().
				Str("jkt", proofClaims.JWKThumbprint).
				Msg("refresh token replay detected! device grant family revoked")
			writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/invalid_grant", "Invalid Grant", problemcode.CodeUnauthorized, "Refresh token replay detected; device grant family revoked", nil)
			return
		}
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/invalid_grant", "Invalid Grant", problemcode.CodeUnauthorized, "Device refresh token invalid or revoked", nil)
		return
	}

	// Credentials must never be cached, by a proxy or by the client.
	w.Header().Set("Cache-Control", "no-store, no-cache, private")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, deviceGrantResponse(grantRes))
}

// webAuthnRequestOptions renders the ceremony options in the shape the contract
// declares. An explicit boundary rather than encoding the webauthn package's
// type directly: that type is free to grow fields the contract has not
// published, and this is the endpoint where a client learns what to sign.
func webAuthnRequestOptions(opts *webauthn.RequestOptions) WebAuthnRequestOptions {
	out := WebAuthnRequestOptions{
		Challenge:        opts.Challenge,
		Timeout:          opts.Timeout,
		RpId:             opts.RPID,
		UserVerification: opts.UserVerification,
	}
	if len(opts.AllowCredentials) == 0 {
		return out
	}

	allowed := make([]WebAuthnCredentialDescriptor, 0, len(opts.AllowCredentials))
	for _, credential := range opts.AllowCredentials {
		descriptor := WebAuthnCredentialDescriptor{Type: credential.Type, Id: credential.ID}
		if len(credential.Transports) > 0 {
			transports := append([]string(nil), credential.Transports...)
			descriptor.Transports = &transports
		}
		allowed = append(allowed, descriptor)
	}
	out.AllowCredentials = &allowed
	return out
}

// domainAssertion converts the wire assertion into the ceremony's own type.
func domainAssertion(assertion WebAuthnAssertionResponse) webauthn.AssertionResponse {
	return webauthn.AssertionResponse{
		CredentialID:      assertion.Id,
		ClientDataJSON:    assertion.ClientDataJSON,
		AuthenticatorData: assertion.AuthenticatorData,
		Signature:         assertion.Signature,
		UserHandle:        stringValue(assertion.UserHandle),
	}
}

// deviceGrantResponse renders an issued grant in the shape DeviceGrantResponse
// declares.
//
// Both issuance paths hand out the same thing — passkey enrollment at
// /auth/device/grant/finish and rotation at /auth/device/refresh — but only the
// second is declared in api/openapi.yaml. Sharing the mapping is what keeps the
// undeclared one from drifting away from the shape its sibling is held to; it
// also means bringing the grant endpoints into the contract later changes the
// routing, not the bytes.
func deviceGrantResponse(grant *identity.DeviceGrantResult) DeviceGrantResponse {
	return DeviceGrantResponse{
		TokenType:    grant.TokenType,
		AccessToken:  grant.AccessToken,
		RefreshToken: grant.RefreshToken,
		ExpiresIn:    clampTokenLifetimeSeconds(grant.ExpiresIn),
		DeviceId:     grant.DeviceID,
		Scope:        grant.Scope,
	}
}
