// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
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

// RegisterPasskeyRoutesWithRegistrar registers the handwritten v3 routes with policy governance.
// It is the daemon's entry point; the route list itself lives in routes_handwritten.go so that
// this path and the chi path in NewHandler cannot diverge.
func RegisterPasskeyRoutesWithRegistrar(registrar RouteRegistrar, svc *Server) error {
	return registerHandwrittenRoutes(registrar, svc)
}

// mountPasskeyRoutes registers the handwritten v3 routes onto a chi router. It is the
// NewHandler entry point and shares its route list with RegisterPasskeyRoutesWithRegistrar.
func (s *Server) mountPasskeyRoutes(r chi.Router) error {
	return registerHandwrittenRoutes(chiRouteRegistrar{router: r}, s)
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
func (s *Server) PasskeyRegisterStart(w http.ResponseWriter, r *http.Request, params PasskeyRegisterStartParams) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	hasUsers, err := svc.HasUsers(r.Context())
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to check existing users")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal_error", "Internal Error", problemcode.CodeInternalServerError, "Failed to check existing users", nil)
		return
	}

	var opts *webauthn.CreationOptions
	p := s.resolveRequestPrincipal(r)

	if hasUsers {
		// When users exist, registration MUST be authenticated!
		if p == nil || p.User == "" {
			writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/authentication_required", "Authentication Required", problemcode.CodeUnauthorized, "You must be authenticated to register a passkey", nil)
			return
		}
		u, err := svc.Store().GetUserByUsername(r.Context(), p.User)
		if err != nil || u == nil {
			writeRegisteredProblem(w, r, http.StatusNotFound, "auth/user_not_found", "User Not Found", problemcode.CodeNotFound, "User not found", nil)
			return
		}
		opts, err = svc.BeginPasskeyRegistration(r.Context(), u.ID)
		if err != nil {
			log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin passkey registration")
			writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/registration_failed", "Registration Failed", problemcode.CodeInvalidInput, err.Error(), nil)
			return
		}
	} else {
		// Bootstrap mode (0 users exist): require valid setup token or operator authorization
		setupToken := stringValue(params.XSetupToken)
		isOperator := principalHasScope(p, "*")

		if !isOperator && setupToken == "" {
			writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/setup_unauthorized", "Setup Not Authorized", problemcode.CodeUnauthorized, "Valid setup token (X-Setup-Token) or operator authorization required to initialize the server", nil)
			return
		}

		if setupToken != "" {
			valid, err := svc.ValidateAndConsumeBootstrapToken(r.Context(), setupToken)
			if err != nil || !valid {
				writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/setup_token_invalid", "Invalid Setup Token", problemcode.CodeUnauthorized, "The provided setup token is expired or invalid", nil)
				return
			}
		}

		opts, err = svc.BeginBootstrapRegistration(r.Context(), "admin", "Administrator")
		if err != nil {
			log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin bootstrap registration")
			writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/bootstrap_failed", "Bootstrap Failed", problemcode.CodeInvalidInput, err.Error(), nil)
			return
		}
	}

	writeJSON(w, http.StatusOK, webAuthnCreationOptions(opts))
}

// PasskeyRegisterFinish handles POST /api/v3/auth/passkey/register/finish
func (s *Server) PasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req PasskeyRegisterFinishRequest
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

	cred, bootstrapRes, err := svc.FinishPasskeyRegistration(r.Context(), domainAttestation(req.Response), stringValue(req.Nickname), userAgent, ipStr)
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

	if bootstrapRes != nil {
		// Set session cookie for the new admin
		s.setSessionCookieDirect(w, r, bootstrapRes.SessionID, bootstrapRes.ExpiresAt)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		credential := passkeyCredentialSummary(cred)
		recoveryCodes := append([]string(nil), bootstrapRes.RecoveryCodes...)
		user := identityUser(bootstrapRes.User)
		expiresAt := bootstrapRes.ExpiresAt.UTC()
		writeJSON(w, http.StatusOK, PasskeyRegistrationResult{
			Status:        BootstrapCompleted,
			User:          &user,
			Credential:    &credential,
			RecoveryCodes: &recoveryCodes,
			ExpiresAt:     &expiresAt,
		})
		return
	}

	summary := passkeyCredentialSummary(cred)
	writeJSON(w, http.StatusOK, PasskeyRegistrationResult{
		Status:         Registered,
		Id:             &summary.Id,
		Nickname:       &summary.Nickname,
		BackupEligible: &summary.BackupEligible,
		BackupState:    &summary.BackupState,
		CreatedAt:      &summary.CreatedAt,
	})
}

// PasskeyLoginStart handles POST /api/v3/auth/passkey/login/start
func (s *Server) PasskeyLoginStart(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req PasskeyLoginStartRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	opts, err := svc.BeginPasskeyLogin(r.Context(), stringValue(req.Username))
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to begin passkey login")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/login_failed", "Login Failed", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, webAuthnRequestOptions(opts))
}

// PasskeyLoginFinish handles POST /api/v3/auth/passkey/login/finish
func (s *Server) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req PasskeyLoginFinishRequest
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

	webSess, user, err := svc.FinishPasskeyLogin(r.Context(), domainAssertion(req.Response), userAgent, ipStr)
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

	writeJSON(w, http.StatusOK, AuthSessionResponse{
		User:      identityUser(*user),
		ExpiresAt: webSess.ExpiresAt.UTC(),
	})
}

// domainAttestation converts the wire attestation into the ceremony's own type.
func domainAttestation(response WebAuthnAttestationResponse) webauthn.AttestationResponse {
	out := webauthn.AttestationResponse{
		ClientDataJSON:    response.ClientDataJSON,
		AttestationObject: response.AttestationObject,
	}
	if response.Transports != nil {
		out.Transports = append([]string(nil), *response.Transports...)
	}
	return out
}

// passkeyCredentialSummary renders the subset of a credential the API publishes.
// The domain type also carries the public key, the AAGUID and the signature
// counter, none of which a client has any use for.
func passkeyCredentialSummary(cred *identity.PasskeyCredential) PasskeyCredentialSummary {
	return PasskeyCredentialSummary{
		Id:             cred.ID,
		Nickname:       cred.Nickname,
		BackupEligible: cred.BackupEligible,
		BackupState:    cred.BackupState,
		CreatedAt:      cred.CreatedAt.UTC(),
	}
}

// identityUser renders a domain user in the shape the contract declares. An
// explicit boundary: identity.User is free to grow fields the API has not
// published, and this type is returned by every login path.
func identityUser(user identity.User) IdentityUser {
	return IdentityUser{
		Id:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        string(user.Role),
		CreatedAt:   user.CreatedAt.UTC(),
		UpdatedAt:   user.UpdatedAt.UTC(),
	}
}

// webAuthnCreationOptions renders registration options in the published shape.
func webAuthnCreationOptions(opts *webauthn.CreationOptions) WebAuthnCreationOptions {
	params := make([]WebAuthnPubKeyCredParam, 0, len(opts.PubKeyCredParams))
	for _, param := range opts.PubKeyCredParams {
		params = append(params, WebAuthnPubKeyCredParam{Type: param.Type, Alg: param.Alg})
	}
	return WebAuthnCreationOptions{
		Rp: WebAuthnRelyingParty{
			Id:   opts.RP.ID,
			Name: opts.RP.Name,
		},
		User: WebAuthnUserEntity{
			Id:          opts.User.ID,
			Name:        opts.User.Name,
			DisplayName: opts.User.DisplayName,
		},
		Challenge:        opts.Challenge,
		PubKeyCredParams: params,
		Timeout:          opts.Timeout,
		AuthenticatorSelection: WebAuthnAuthenticatorSelection{
			ResidentKey:      opts.AuthenticatorSelection.ResidentKey,
			UserVerification: opts.AuthenticatorSelection.UserVerification,
		},
		Attestation: opts.Attestation,
	}
}

// RecoveryLogin handles POST /api/v3/auth/recovery
func (s *Server) RecoveryLogin(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/passkey_disabled", "Passkey Not Configured", problemcode.CodeServiceUnavailable, "Passkey authentication is not configured on this server", nil)
		return
	}

	var req RecoveryLoginRequest
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

	writeJSON(w, http.StatusOK, AuthSessionResponse{
		User:      identityUser(*user),
		ExpiresAt: webSess.ExpiresAt.UTC(),
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
		if result := s.TokenPrincipal(r.Context(), token); result.OK() {
			return result.Principal
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

// AcknowledgeRecovery handles POST /api/v3/auth/bootstrap/acknowledge-recovery
func (s *Server) AcknowledgeRecovery(w http.ResponseWriter, r *http.Request) {
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
	if err != nil || user == nil {
		writeRegisteredProblem(w, r, http.StatusNotFound, "auth/user_not_found", "User Not Found", problemcode.CodeNotFound, "User not found", nil)
		return
	}

	if err := svc.AcknowledgeRecoveryCodes(r.Context(), user.ID); err != nil {
		log.FromContext(r.Context()).Error().Err(err).Msg("failed to acknowledge recovery codes")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/acknowledgment_failed", "Acknowledgment Failed", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	log.FromContext(r.Context()).Info().
		Str("user_id", user.ID).
		Str("username", user.Username).
		Msg("recovery codes acknowledged and bootstrap closed")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "ready",
		"publicReady":     true,
		"bootstrapClosed": true,
	})
}

// AuthStatus handles GET /api/v3/auth/status
func (s *Server) AuthStatus(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured":    false,
			"identityReady": false,
			"publicReady":   false,
		})
		return
	}

	identityReady, _ := svc.IsIdentityReady(r.Context())
	publicReady, _ := svc.IsPublicReady(r.Context())
	state, _ := svc.GetBootstrapStatus(r.Context())
	meta, _ := svc.Store().GetBootstrapMeta(r.Context())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"configured":           true,
		"identityReady":        identityReady,
		"publicReady":          publicReady,
		"bootstrapState":       string(state),
		"setupRequired":        state == identity.BootstrapStateSetupRequired,
		"recoveryAcknowledged": meta != nil && meta.RecoveryCodesAcknowledgedAt != nil,
		"rpId":                 svc.Config().RPID,
	})
}
