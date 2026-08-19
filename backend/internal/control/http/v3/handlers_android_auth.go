// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/dpop"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type DeviceGrantStartRequest struct {
	Username string `json:"username,omitempty"`
}

type DeviceGrantFinishRequest struct {
	Assertion  webauthn.AssertionResponse `json:"assertion"`
	DeviceName string                     `json:"deviceName"`
	Platform   string                     `json:"platform"`
	DeviceJWK  dpop.JWKECPublicKey        `json:"deviceJwk"`
	Scopes     string                     `json:"scopes,omitempty"`
}

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

	opts, err := svc.BeginPasskeyLogin(r.Context(), req.Username)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin device passkey assertion")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/login_failed", "Assertion Failed", problemcode.CodeInvalidInput, "Failed to begin passkey assertion", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(opts)
}

// DeviceGrantFinish handles POST /api/v3/auth/device/grant/finish
func (s *Server) DeviceGrantFinish(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	dpopProof := r.Header.Get("DPoP")
	if dpopProof == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/dpop_required", "DPoP Header Required", problemcode.CodeInvalidInput, "DPoP proof header is required", nil)
		return
	}

	validator := s.getDPoPValidator()
	now := time.Now().UTC()
	proofClaims, err := validator.ValidateProof(r, dpopProof, "", now)
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
	user, _, err := svc.VerifyPasskeyAssertion(r.Context(), req.Assertion)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("passkey assertion failed during android device grant")
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/login_failed", "Authentication Failed", problemcode.CodeUnauthorized, "Invalid passkey assertion", nil)
		return
	}

	if req.DeviceName == "" {
		req.DeviceName = "Android Device"
	}
	if req.Platform == "" {
		req.Platform = "android"
	}

	// 2. Issue DPoP-bound Device Grant & Access Token
	grantRes, err := svc.IssueDeviceGrant(r.Context(), user.ID, req.DeviceName, req.Platform, proofClaims.Header.JWK, req.Scopes, identity.GrantTypePasskeyEnrollment)
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, no-cache, private")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(grantRes)
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
	writeJSON(w, http.StatusOK, DeviceGrantResponse{
		TokenType:    grantRes.TokenType,
		AccessToken:  grantRes.AccessToken,
		RefreshToken: grantRes.RefreshToken,
		ExpiresIn:    clampTokenLifetimeSeconds(grantRes.ExpiresIn),
		DeviceId:     grantRes.DeviceID,
		Scope:        grantRes.Scope,
	})
}
