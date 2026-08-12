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

type DeviceRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// DeviceGrantStart handles POST /api/v3/auth/device/grant/start
func (s *Server) DeviceGrantStart(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		http.Error(w, `{"error":"identity_service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var req DeviceGrantStartRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	opts, err := svc.BeginPasskeyLogin(r.Context(), req.Username)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin device passkey assertion")
		http.Error(w, `{"error":"failed_to_begin_assertion"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(opts)
}

// DeviceGrantFinish handles POST /api/v3/auth/device/grant/finish
func (s *Server) DeviceGrantFinish(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		http.Error(w, `{"error":"identity_service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	dpopProof := r.Header.Get("DPoP")
	if dpopProof == "" {
		http.Error(w, `{"error":"dpop_header_required"}`, http.StatusBadRequest)
		return
	}

	validator := s.getDPoPValidator()
	now := time.Now().UTC()
	proofClaims, err := validator.ValidateProof(r, dpopProof, "", now)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("invalid DPoP proof during device grant finish")
		http.Error(w, `{"error":"invalid_dpop_proof","detail":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	var req DeviceGrantFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json_body"}`, http.StatusBadRequest)
		return
	}

	// 1. Verify User Identity via Passkey WITHOUT setting browser cookies
	user, _, err := svc.VerifyPasskeyAssertion(r.Context(), req.Assertion)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("passkey assertion failed during android device grant")
		http.Error(w, `{"error":"invalid_passkey_assertion"}`, http.StatusUnauthorized)
		return
	}

	if req.DeviceName == "" {
		req.DeviceName = "Android Device"
	}
	if req.Platform == "" {
		req.Platform = "android"
	}

	// 2. Issue DPoP-bound Device Grant & Access Token
	grantRes, err := svc.IssueDeviceGrant(r.Context(), user.ID, req.DeviceName, req.Platform, proofClaims.Header.JWK, req.Scopes)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to issue device grant")
		http.Error(w, `{"error":"failed_to_issue_grant"}`, http.StatusInternalServerError)
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
func (s *Server) DeviceRefresh(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		http.Error(w, `{"error":"identity_service_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	dpopProof := r.Header.Get("DPoP")
	if dpopProof == "" {
		http.Error(w, `{"error":"dpop_header_required"}`, http.StatusBadRequest)
		return
	}

	validator := s.getDPoPValidator()
	now := time.Now().UTC()
	proofClaims, err := validator.ValidateProof(r, dpopProof, "", now)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("invalid DPoP proof during device refresh")
		http.Error(w, `{"error":"invalid_dpop_proof","detail":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	var req DeviceRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		http.Error(w, `{"error":"invalid_refresh_token_payload"}`, http.StatusBadRequest)
		return
	}

	grantRes, err := svc.RotateDeviceRefreshToken(r.Context(), req.RefreshToken, proofClaims.JWKThumbprint)
	if err != nil {
		if errors.Is(err, identity.ErrRefreshTokenReplay) {
			log.FromContext(r.Context()).Error().
				Str("jkt", proofClaims.JWKThumbprint).
				Msg("refresh token replay detected! device grant family revoked")
			http.Error(w, `{"error":"invalid_grant","detail":"refresh token replay detected; device grant family revoked"}`, http.StatusUnauthorized)
			return
		}
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, no-cache, private")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(grantRes)
}
