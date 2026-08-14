// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// ListNotifications handles GET /api/v3/notifications
func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principalID := s.resolvePrincipalID(r)
	unreadOnly := r.URL.Query().Get("unreadOnly") == "true"

	notifs, err := svc.ListNotifications(r.Context(), "default_household", principalID, unreadOnly)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to list notifications", nil)
		return
	}

	if notifs == nil {
		notifs = []identity.Notification{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(notifs)
}

// StreamNotifications handles GET /api/v3/notifications/stream (SSE)
func (s *Server) StreamNotifications(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Streaming Unsupported", problemcode.CodeInvalidInput, "Streaming unsupported", nil)
		return
	}

	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principalID := s.resolvePrincipalID(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Send initial connection event
	notifs, _ := svc.ListNotifications(r.Context(), "default_household", principalID, true)
	unreadCount := len(notifs)
	_, _ = fmt.Fprintf(w, "event: connected\ndata: {\"unreadCount\":%d,\"userId\":\"%s\"}\n\n", unreadCount, principalID)
	flusher.Flush()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	lastUnread := unreadCount

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			currentNotifs, err := svc.ListNotifications(r.Context(), "default_household", principalID, true)
			if err == nil {
				currentUnread := len(currentNotifs)
				if currentUnread != lastUnread {
					lastUnread = currentUnread
					payload, _ := json.Marshal(map[string]any{
						"unreadCount":   currentUnread,
						"notifications": currentNotifs,
					})
					_, _ = fmt.Fprintf(w, "event: notification\ndata: %s\n\n", payload)
					flusher.Flush()
				}
			}
		}
	}
}

// MarkNotificationRead handles POST /api/v3/notifications/mark-read
func (s *Server) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principalID := s.resolvePrincipalID(r)

	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.ID == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Notification ID", problemcode.CodeInvalidInput, "Notification ID is required", nil)
		return
	}

	if err := svc.MarkNotificationRead(r.Context(), payload.ID, principalID); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to mark notification read", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// MarkAllNotificationsRead handles POST /api/v3/notifications/mark-all-read
func (s *Server) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principalID := s.resolvePrincipalID(r)

	if err := svc.MarkAllNotificationsRead(r.Context(), "default_household", principalID); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to mark all notifications read", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteNotification handles DELETE /api/v3/notifications/{id}
func (s *Server) DeleteNotification(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principalID := s.resolvePrincipalID(r)
	id := r.PathValue("id")
	if id == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Notification ID", problemcode.CodeInvalidInput, "Notification ID is required", nil)
		return
	}

	if err := svc.DeleteNotification(r.Context(), id, principalID); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to delete notification", nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetVAPIDPublicKey handles GET /api/v3/notifications/vapid-key
func (s *Server) GetVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	keys, err := identity.GetOrGenerateVAPIDKeys(".")
	pubKey := "BEl62iUYgUivxIbcLqWVmNs0FGH5k2v8JpX9qLmZ5uN6kW9yX_2v8JpX9qLmZ5uN6kW9yX"
	if err == nil && keys != nil && keys.PublicKey != "" {
		pubKey = keys.PublicKey
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"publicKey": pubKey,
	})
}

// SavePushSubscription handles POST /api/v3/notifications/push-subscriptions
func (s *Server) SavePushSubscription(w http.ResponseWriter, r *http.Request) {
	svc := s.getIdentityService()
	if svc == nil {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "auth/disabled", "Identity Service Unavailable", problemcode.CodeServiceUnavailable, "Identity service is not configured", nil)
		return
	}

	principalID := s.resolvePrincipalID(r)

	var payload struct {
		Endpoint string `json:"endpoint"`
		Channel  string `json:"channel"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Endpoint == "" {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "request/invalid", "Invalid Push Payload", problemcode.CodeInvalidInput, "Valid push subscription payload required", nil)
		return
	}

	channel := payload.Channel
	if channel == "" {
		channel = "webpush"
	}

	sub := &identity.PushSubscription{
		HouseholdID: "default_household",
		UserID:      principalID,
		Endpoint:    payload.Endpoint,
		P256dh:      payload.Keys.P256dh,
		Auth:        payload.Keys.Auth,
		UserAgent:   r.UserAgent(),
		Channel:     channel,
		CreatedAt:   time.Now(),
	}

	if err := svc.SavePushSubscription(r.Context(), sub); err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Failed to save push subscription", nil)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) resolvePrincipalID(r *http.Request) string {
	if u := r.URL.Query().Get("userId"); u != "" {
		return u
	}
	if cookie, err := r.Cookie("xg2g_session"); err == nil && cookie.Value != "" {
		if token, ok := s.authSessionStoreOrDefault().ResolveSessionToken(cookie.Value); ok && token != "" {
			return token
		}
	}
	return "usr_admin"
}
