// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/go-chi/chi/v5"
)

// RegisterPasskeyRoutesWithRegistrar registers passkey identity routes with policy governance.
func RegisterPasskeyRoutesWithRegistrar(registrar RouteRegistrar, svc *Server) error {
	if registrar == nil || svc == nil {
		return fmt.Errorf("nil registrar or server")
	}
	if err := registrar.Register(http.MethodPost, "/auth/passkey/login/start", http.HandlerFunc(svc.PasskeyLoginStart)); err != nil {
		return err
	}
	if err := registrar.Register(http.MethodPost, "/auth/passkey/login/finish", http.HandlerFunc(svc.PasskeyLoginFinish)); err != nil {
		return err
	}
	if err := registrar.Register(http.MethodPost, "/auth/passkey/register/start", http.HandlerFunc(svc.PasskeyRegisterStart)); err != nil {
		return err
	}
	if err := registrar.Register(http.MethodPost, "/auth/passkey/register/finish", http.HandlerFunc(svc.PasskeyRegisterFinish)); err != nil {
		return err
	}
	if err := registrar.Register(http.MethodPost, "/auth/recovery", http.HandlerFunc(svc.RecoveryLogin)); err != nil {
		return err
	}
	if err := registrar.Register(http.MethodGet, "/auth/passkeys", svc.authMiddleware(http.HandlerFunc(svc.ListPasskeys))); err != nil {
		return err
	}
	if err := registrar.Register(http.MethodDelete, "/auth/passkeys/{id}", svc.authMiddleware(http.HandlerFunc(svc.DeletePasskey))); err != nil {
		return err
	}
	return registrar.Register(http.MethodPost, "/auth/sessions/revoke-others", svc.authMiddleware(http.HandlerFunc(svc.RevokeOtherSessions)))
}

func (s *Server) mountPasskeyRoutes(r chi.Router) {
	r.Route("/auth/passkey", func(pr chi.Router) {
		pr.Post("/login/start", s.PasskeyLoginStart)
		pr.Post("/login/finish", s.PasskeyLoginFinish)
		pr.Post("/register/start", s.PasskeyRegisterStart)
		pr.Post("/register/finish", s.PasskeyRegisterFinish)
	})
	r.Post("/auth/recovery", s.RecoveryLogin)

	// Protected management endpoints
	r.Group(func(pr chi.Router) {
		pr.Use(s.authMiddleware)
		pr.Get("/auth/passkeys", s.ListPasskeys)
		pr.Delete("/auth/passkeys/{id}", s.DeletePasskey)
		pr.Post("/auth/sessions/revoke-others", s.RevokeOtherSessions)
	})
}

func (s *Server) getIdentityService() *identity.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identityService
}

func (s *Server) SetIdentityService(svc *identity.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identityService = svc
}

// PasskeyRegisterStart handles POST /api/v3/auth/passkey/register/start
func (s *Server) PasskeyRegisterStart(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	// 1. Resolve current authenticated user or bootstrap admin
	userID := ""
	if p := s.resolveRequestPrincipal(r); p != nil && p.User != "" {
		if u, err := svc.Store().GetUserByUsername(r.Context(), p.User); err == nil && u != nil {
			userID = u.ID
		}
	}

	if userID == "" {
		// Bootstrap check: if database has 0 users, allow bootstrap registration
		admin, _, err := svc.EnsureDefaultAdminUser(r.Context(), "admin", "Administrator")
		if err != nil {
			log.FromContext(r.Context()).Error().Err(err).Msg("failed to ensure default admin user")
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "auth/bootstrap_failed", "Bootstrap Failed", problemcode.CodeInternalServerError, "Failed to initialize default user", nil)
			return
		}
		userID = admin.ID
	}

	opts, err := svc.BeginPasskeyRegistration(r.Context(), userID)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin passkey registration")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/registration_failed", "Registration Failed", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(opts)
}

type FinishRegistrationRequest struct {
	Response webauthn.AttestationResponse `json:"response"`
	Nickname string                       `json:"nickname"`
}

// PasskeyRegisterFinish handles POST /api/v3/auth/passkey/register/finish
func (s *Server) PasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req FinishRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, "Invalid JSON payload", nil)
		return
	}

	cred, err := svc.FinishPasskeyRegistration(r.Context(), req.Response, req.Nickname)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("passkey registration verification failed")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/registration_failed", "Registration Verification Failed", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	log.FromContext(r.Context()).Info().
		Str("credential_id", cred.ID).
		Str("user_id", cred.UserID).
		Bool("backup_eligible", cred.BackupEligible).
		Bool("backup_state", cred.BackupState).
		Msg("passkey credential registered successfully")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":             cred.ID,
		"nickname":       cred.Nickname,
		"backupEligible": cred.BackupEligible,
		"backupState":    cred.BackupState,
		"createdAt":      cred.CreatedAt,
	})
}

type LoginStartRequest struct {
	Username string `json:"username,omitempty"`
}

// PasskeyLoginStart handles POST /api/v3/auth/passkey/login/start
func (s *Server) PasskeyLoginStart(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req LoginStartRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	opts, err := svc.BeginPasskeyLogin(r.Context(), req.Username)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin passkey login")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/login_failed", "Login Failed", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(opts)
}

type LoginFinishRequest struct {
	Response webauthn.AssertionResponse `json:"response"`
}

type AuthSessionResponse struct {
	SessionID string        `json:"sessionId"`
	User      identity.User `json:"user"`
	ExpiresAt time.Time     `json:"expiresAt"`
}

// PasskeyLoginFinish handles POST /api/v3/auth/passkey/login/finish
func (s *Server) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req LoginFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, "Invalid JSON payload", nil)
		return
	}

	userAgent := r.UserAgent()
	remoteIP := requestRemoteIP(r)
	ipStr := ""
	if remoteIP != nil {
		ipStr = remoteIP.String()
	}

	webSess, user, err := svc.FinishPasskeyLogin(r.Context(), req.Response, userAgent, ipStr)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Msg("passkey assertion failed")
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/login_failed", "Authentication Failed", problemcode.CodeUnauthorized, err.Error(), nil)
		return
	}

	s.setSessionCookieDirect(w, r, webSess.SessionID, webSess.ExpiresAt)

	log.FromContext(r.Context()).Info().
		Str("user_id", user.ID).
		Str("username", user.Username).
		Str("session_id", webSess.SessionID).
		Msg("passkey login successful")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AuthSessionResponse{
		SessionID: webSess.SessionID,
		User:      *user,
		ExpiresAt: webSess.ExpiresAt,
	})
}

type RecoveryRequest struct {
	Username string `json:"username"`
	Code     string `json:"code"`
}

// RecoveryLogin handles POST /api/v3/auth/recovery
func (s *Server) RecoveryLogin(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req RecoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, "Invalid JSON payload", nil)
		return
	}

	if req.Username == "" || req.Code == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/invalid_recovery", "Invalid Recovery Code", problemcode.CodeInvalidInput, "Username and recovery code are required", nil)
		return
	}

	userAgent := r.UserAgent()
	remoteIP := requestRemoteIP(r)
	ipStr := ""
	if remoteIP != nil {
		ipStr = remoteIP.String()
	}

	webSess, user, err := svc.LoginWithRecoveryCode(r.Context(), req.Username, req.Code, userAgent, ipStr)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Str("username", req.Username).Msg("recovery code login failed")
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/invalid_recovery", "Invalid Recovery Code", problemcode.CodeUnauthorized, "The recovery code is invalid or already consumed", nil)
		return
	}

	s.setSessionCookieDirect(w, r, webSess.SessionID, webSess.ExpiresAt)

	log.FromContext(r.Context()).Warn().
		Str("user_id", user.ID).
		Str("username", user.Username).
		Str("session_id", webSess.SessionID).
		Msg("recovery code authenticated and consumed")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AuthSessionResponse{
		SessionID: webSess.SessionID,
		User:      *user,
		ExpiresAt: webSess.ExpiresAt,
	})
}

// ListPasskeys handles GET /api/v3/auth/passkeys
func (s *Server) ListPasskeys(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	p := s.resolveRequestPrincipal(r)
	if p == nil || p.User == "" {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	user, err := svc.Store().GetUserByUsername(r.Context(), p.User)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusNotFound, "auth/user_not_found", "User Not Found", problemcode.CodeNotFound, "User not found", nil)
		return
	}

	creds, err := svc.Store().ListPasskeysByUser(r.Context(), user.ID)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to list passkeys")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalServerError, "Failed to list passkeys", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(creds)
}

// DeletePasskey handles DELETE /api/v3/auth/passkeys/{id}
func (s *Server) DeletePasskey(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, "Passkey ID required", nil)
		return
	}

	p := s.resolveRequestPrincipal(r)
	if p == nil || p.User == "" {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	user, err := svc.Store().GetUserByUsername(r.Context(), p.User)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusNotFound, "auth/user_not_found", "User Not Found", problemcode.CodeNotFound, "User not found", nil)
		return
	}

	cred, err := svc.Store().GetPasskey(r.Context(), id)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusNotFound, "auth/passkey_not_found", "Passkey Not Found", problemcode.CodeNotFound, "Passkey not found", nil)
		return
	}

	if cred.UserID != user.ID && user.Role != identity.RoleAdmin {
		writeRegisteredProblem(w, r, http.StatusForbidden, "auth/forbidden", "Forbidden", problemcode.CodeForbidden, "Cannot delete passkey of another user", nil)
		return
	}

	if err := svc.Store().DeletePasskey(r.Context(), id); err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to delete passkey")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalServerError, "Failed to delete passkey", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RevokeOtherSessions handles POST /api/v3/auth/sessions/revoke-others
func (s *Server) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	p := s.resolveRequestPrincipal(r)
	if p == nil || p.User == "" {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	user, err := svc.Store().GetUserByUsername(r.Context(), p.User)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusNotFound, "auth/user_not_found", "User Not Found", problemcode.CodeNotFound, "User not found", nil)
		return
	}

	currentSessionID := p.ID
	if err := svc.RevokeOtherWebSessions(r.Context(), user.ID, currentSessionID); err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to revoke other sessions")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalServerError, "Failed to revoke other sessions", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolveRequestPrincipal(r *http.Request) *auth.Principal {
	if r == nil {
		return nil
	}
	if p := auth.PrincipalFromContext(r.Context()); p != nil {
		return p
	}
	cfg := s.GetConfig()
	token, _ := s.extractTokenDetailedWithLegacyPolicy(r, !cfg.APIDisableLegacyTokenSources)
	if token != "" {
		if p, ok := s.TokenPrincipal(r.Context(), token); ok {
			return p
		}
	}
	return nil
}

func (s *Server) setSessionCookieDirect(w http.ResponseWriter, r *http.Request, sessionID string, expiresAt time.Time) {
	effectiveHTTPS := s.requestIsHTTPS(r)
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge <= 0 {
		maxAge = sessionCookieMaxAgeSeconds
	}
	setServerCookie(w, &http.Cookie{ // #nosec G124 -- HttpOnly+SameSite=Lax set
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/api/v3/",
		HttpOnly: true,
		Secure:   effectiveHTTPS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}
