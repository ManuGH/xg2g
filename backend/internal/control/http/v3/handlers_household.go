// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/go-chi/chi/v5"
)

type PasswordLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateInvitationRequest struct {
	Role        string `json:"role"`
	DisplayName string `json:"displayName,omitempty"`
}

type CreateInvitationResponse struct {
	InviteCode  string    `json:"inviteCode"`
	InviteURL   string    `json:"inviteUrl"`
	Role        string    `json:"role"`
	DisplayName string    `json:"displayName,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type RedeemInvitationRequest struct {
	InviteCode  string `json:"inviteCode"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Password    string `json:"password"`
}

type CreateProfileRequest struct {
	Name            string   `json:"name"`
	AvatarURL       string   `json:"avatarUrl,omitempty"`
	IsChild         bool     `json:"isChild"`
	AllowedBouquets []string `json:"allowedBouquets,omitempty"`
	BlockedChannels []string `json:"blockedChannels,omitempty"`
	MaturityLevel   int      `json:"maturityLevel,omitempty"`
	ExitPIN         string   `json:"exitPin,omitempty"`
}

func (s *Server) mountHouseholdRoutes(r chi.Router) {
	r.Post("/auth/login/password", s.PasswordLogin)
	r.Post("/auth/invitations/redeem", s.RedeemInvitation)

	r.Group(func(pr chi.Router) {
		pr.Use(s.authMiddleware)
		pr.Post("/auth/invitations", s.CreateInvitation)
		pr.Get("/auth/effective-permissions", s.GetEffectivePermissions)
		pr.Get("/profiles", s.ListProfiles)
		pr.Post("/profiles", s.CreateProfile)
		pr.Get("/profiles/{id}", s.GetProfile)
		pr.Delete("/profiles/{id}", s.DeleteProfile)

		pr.Get("/household/policies/access", s.GetAccessPolicy)
		pr.Post("/household/policies/access", s.CreateAccessPolicy)
		pr.Post("/household/policies/access/revoke", s.RevokeAccessPolicy)
		pr.Get("/household/approvals", s.ListApprovalRequests)
		pr.Post("/household/approvals", s.CreateApprovalRequest)
		pr.Post("/household/approvals/{id}/approve", s.ApproveApprovalRequest)
		pr.Post("/household/approvals/{id}/deny", s.DenyApprovalRequest)
		pr.Get("/household/resource-policy", s.GetHouseholdResourcePolicy)
		pr.Put("/household/resource-policy", s.PutHouseholdResourcePolicy)
		pr.Post("/sessions/revoke-user-sessions", s.RevokeUserSessions)

		pr.Get("/notifications", s.ListNotifications)
		pr.Get("/notifications/stream", s.StreamNotifications)
		pr.Post("/notifications/mark-read", s.MarkNotificationRead)
		pr.Post("/notifications/mark-all-read", s.MarkAllNotificationsRead)
		pr.Delete("/notifications/{id}", s.DeleteNotification)
		pr.Get("/notifications/vapid-key", s.GetVAPIDPublicKey)
		pr.Post("/notifications/push-subscriptions", s.SavePushSubscription)
	})
}

// PasswordLogin handles POST /api/v3/auth/login/password
func (s *Server) PasswordLogin(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	var req PasswordLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Body", problemcode.CodeInvalidInput, "Failed to parse JSON body", nil)
		return
	}

	res, err := svc.AuthenticateWithPassword(r.Context(), req.Username, req.Password)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Str("username", req.Username).Msg("password login failed")
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/invalid_credentials", "Invalid Credentials", problemcode.CodeUnauthorized, "Invalid username or password", nil)
		return
	}

	s.setSessionCookieDirect(w, r, res.SessionID, res.ExpiresAt)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// CreateInvitation handles POST /api/v3/auth/invitations
func (s *Server) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principal := s.resolveRequestPrincipal(r)
	if principal == nil {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Body", problemcode.CodeInvalidInput, "Failed to parse JSON body", nil)
		return
	}

	role := identity.Role(req.Role)
	if role != identity.RoleMember && role != identity.RoleGuest {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Role", problemcode.CodeInvalidInput, "Role must be member or guest", nil)
		return
	}

	code, inv, err := svc.CreateInvitation(r.Context(), principal.ID, role, req.DisplayName)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusForbidden, "auth/forbidden", "Forbidden", problemcode.CodeForbidden, err.Error(), nil)
		return
	}

	inviteURL := "/auth/invite?code=" + code
	res := CreateInvitationResponse{
		InviteCode:  code,
		InviteURL:   inviteURL,
		Role:        string(inv.Role),
		DisplayName: inv.DisplayName,
		ExpiresAt:   inv.ExpiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// RedeemInvitation handles POST /api/v3/auth/invitations/redeem
func (s *Server) RedeemInvitation(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	var req RedeemInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Body", problemcode.CodeInvalidInput, "Failed to parse JSON body", nil)
		return
	}

	res, err := svc.RedeemInvitationWithPassword(r.Context(), req.InviteCode, req.Username, req.DisplayName, req.Password)
	if err != nil {
		log.FromContext(r.Context()).Warn().Err(err).Str("username", req.Username).Msg("invite redemption failed")
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/invite_failed", "Invite Redemption Failed", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	s.setSessionCookieDirect(w, r, res.SessionID, res.ExpiresAt)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// CreateProfile handles POST /api/v3/profiles
func (s *Server) CreateProfile(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principal := s.resolveRequestPrincipal(r)
	if principal == nil {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	var req CreateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Body", problemcode.CodeInvalidInput, "Failed to parse JSON body", nil)
		return
	}

	prof, pol, err := svc.CreateProfile(r.Context(), principal.ID, req.Name, req.AvatarURL, req.IsChild, req.AllowedBouquets, req.BlockedChannels, req.MaturityLevel, req.ExitPIN)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Failed to Create Profile", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"profile": prof,
		"policy":  pol,
	})
}

// ListProfiles handles GET /api/v3/profiles
func (s *Server) ListProfiles(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	profs, err := svc.Store().ListProfilesByHousehold(r.Context(), "default_household")
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to list profiles", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profs)
}

// GetProfile handles GET /api/v3/profiles/{id}
func (s *Server) GetProfile(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	profID := chi.URLParam(r, "id")
	prof, pol, err := svc.Store().GetProfile(r.Context(), profID)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusNotFound, "system/not_found", "Profile Not Found", problemcode.CodeNotFound, "Profile not found", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"profile": prof,
		"policy":  pol,
	})
}

// DeleteProfile handles DELETE /api/v3/profiles/{id}
func (s *Server) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	profID := chi.URLParam(r, "id")
	if err := svc.Store().DeleteProfile(r.Context(), profID); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to delete profile", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetEffectivePermissions handles GET /api/v3/auth/effective-permissions
func (s *Server) GetEffectivePermissions(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principal := s.resolveRequestPrincipal(r)
	if principal == nil {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	profID := strings.TrimSpace(r.URL.Query().Get("profile_id"))
	eff, err := svc.GetEffectivePermissions(r.Context(), principal.ID, profID)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, err.Error(), nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(eff)
}

// OpenAPI HouseholdServerInterface implementations
func (s *Server) GetHouseholdProfiles(w http.ResponseWriter, r *http.Request, params GetHouseholdProfilesParams) {
	if s.householdService != nil && s.getIdentityService() == nil {
		profs, err := s.householdService.List(r.Context())
		if err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to list profiles", nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(profs)
		return
	}
	s.ListProfiles(w, r)
}

func (s *Server) PostHouseholdProfiles(w http.ResponseWriter, r *http.Request, params PostHouseholdProfilesParams) {
	if s.householdService != nil && s.getIdentityService() == nil {
		var prof household.Profile
		if err := json.NewDecoder(r.Body).Decode(&prof); err != nil {
			writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Invalid profile payload", nil)
			return
		}
		created, err := s.householdService.Save(r.Context(), prof)
		if err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to create profile", nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(created)
		return
	}
	s.CreateProfile(w, r)
}

func (s *Server) DeleteHouseholdProfile(w http.ResponseWriter, r *http.Request, profileId string, params DeleteHouseholdProfileParams) {
	if s.householdService != nil && s.getIdentityService() == nil {
		if err := s.householdService.Delete(r.Context(), profileId); err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to delete profile", nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.DeleteProfile(w, r)
}

func (s *Server) PutHouseholdProfile(w http.ResponseWriter, r *http.Request, profileId string, params PutHouseholdProfileParams) {
	if s.householdService != nil && s.getIdentityService() == nil {
		var prof household.Profile
		if err := json.NewDecoder(r.Body).Decode(&prof); err != nil {
			writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Invalid profile payload", nil)
			return
		}
		prof.ID = profileId
		updated, err := s.householdService.Save(r.Context(), prof)
		if err != nil {
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to update profile", nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(updated)
		return
	}
	s.CreateProfile(w, r)
}

func (s *Server) GetAccessPolicy(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	p := s.resolveRequestPrincipal(r)
	if p == nil || p.User == "" {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}
	user, _ := svc.Store().GetUserByUsername(r.Context(), p.User)
	userID := p.User
	if user != nil {
		userID = user.ID
	}

	targetUserID := r.URL.Query().Get("userId")
	if targetUserID == "" {
		targetUserID = userID
	}

	pol, err := svc.GetAccessPolicy(r.Context(), targetUserID)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to get access policy", nil)
		return
	}
	if pol == nil {
		pol = &identity.AccessPolicy{
			AccountID:         targetUserID,
			DailyStart:        "07:00",
			DailyEnd:          "19:00",
			Timezone:          "Europe/Vienna",
			AllowedDaysMask:   127,
			LiveTVAllowed:     true,
			EPGAllowed:        true,
			DVRAllowed:        true,
			RecordingsAllowed: true,
			MaxDevices:        3,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pol)
}

func (s *Server) CreateAccessPolicy(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	var pol identity.AccessPolicy
	if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Invalid access policy JSON", nil)
		return
	}

	if pol.AccountID == "" {
		p := s.resolveRequestPrincipal(r)
		if p != nil && p.User != "" {
			user, _ := svc.Store().GetUserByUsername(r.Context(), p.User)
			if user != nil {
				pol.AccountID = user.ID
			} else {
				pol.AccountID = p.User
			}
		}
	}

	if err := svc.CreateAccessPolicy(r.Context(), &pol); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to create access policy", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(pol)
}

func (s *Server) RevokeAccessPolicy(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	policyID := r.URL.Query().Get("id")
	if policyID == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Missing id parameter", nil)
		return
	}

	if err := svc.RevokeAccessPolicy(r.Context(), policyID); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to revoke access policy", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListApprovalRequests(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	status := r.URL.Query().Get("status")
	reqs, err := svc.ListApprovalRequests(r.Context(), "default_household", status)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to list approval requests", nil)
		return
	}
	if reqs == nil {
		reqs = []identity.ApprovalRequest{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reqs)
}

type CreateApprovalPayload struct {
	ProfileID      string `json:"profileId"`
	RequestType    string `json:"requestType"`
	ResourceID     string `json:"resourceId"`
	ResourceName   string `json:"resourceName"`
	ParentalRating int    `json:"parentalRating"`
	Scope          string `json:"scope"`
}

func (s *Server) CreateApprovalRequest(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	var p CreateApprovalPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Invalid approval payload", nil)
		return
	}

	appr, err := svc.CreateApprovalRequest(r.Context(), p.ProfileID, p.RequestType, p.ResourceID, p.ResourceName, p.ParentalRating, p.Scope, time.Time{})
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to create approval request", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(appr)
}

func (s *Server) ApproveApprovalRequest(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	id := chi.URLParam(r, "id")
	p := s.resolveRequestPrincipal(r)
	adminID := "admin"
	if p != nil && p.User != "" {
		adminID = p.User
	}
	if err := svc.ApproveRequest(r.Context(), id, adminID); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Approval Error", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) DenyApprovalRequest(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	id := chi.URLParam(r, "id")
	p := s.resolveRequestPrincipal(r)
	adminID := "admin"
	if p != nil && p.User != "" {
		adminID = p.User
	}
	if err := svc.DenyRequest(r.Context(), id, adminID); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Approval Error", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetHouseholdResourcePolicy(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	pol, err := svc.GetHouseholdResourcePolicy(r.Context(), "default_household")
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to get resource policy", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pol)
}

func (s *Server) PutHouseholdResourcePolicy(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	var pol identity.HouseholdResourcePolicy
	if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Invalid resource policy payload", nil)
		return
	}
	if err := svc.PutHouseholdResourcePolicy(r.Context(), &pol); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to update resource policy", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pol)
}

func (s *Server) RevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}
	type RevokeReq struct {
		UserID string `json:"userId"`
	}
	var req RevokeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Request", problemcode.CodeInvalidInput, "Invalid userId", nil)
		return
	}
	if err := svc.RevokeAllUserSessions(r.Context(), req.UserID); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to revoke user sessions", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
