package v3

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

const (
	householdUnlockCookieName = "xg2g_household_unlock"
	defaultHouseholdUnlockTTL = 4 * time.Hour
)

func (s *Server) GetHouseholdUnlock(w http.ResponseWriter, r *http.Request, params GetHouseholdUnlockParams) {
	_ = params
	status := s.currentHouseholdUnlockStatus(s.isHouseholdUnlocked(r))
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) PostHouseholdUnlock(w http.ResponseWriter, r *http.Request, params PostHouseholdUnlockParams) {
	_ = params
	cfg := s.GetConfig()
	if !cfg.Household.PinConfigured() {
		writeRegisteredProblem(w, r, http.StatusConflict, "household/pin_not_configured", "Household Pin Not Configured", problemcode.CodeConflict, "A household pin is not configured", nil)
		return
	}

	var body HouseholdUnlockRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "household/invalid_input", "Invalid Household Pin", problemcode.CodeInvalidInput, "The household unlock request body is malformed", nil)
		return
	}

	matches, err := household.VerifyStoredPIN(cfg.Household.PinHash, body.Pin)
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "household/invalid_input", "Invalid Household Pin", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}
	if !matches {
		writeRegisteredProblem(w, r, http.StatusForbidden, "household/invalid_pin", "Invalid Household Pin", problemcode.CodeForbidden, "The supplied household pin is incorrect", nil)
		return
	}

	store := s.householdUnlockStoreOrDefault()
	if existingCookie, err := r.Cookie(householdUnlockCookieName); err == nil {
		s.deleteHouseholdUnlock(existingCookie.Value)
	}

	sessionID, err := store.CreateUnlock(s.householdUnlockTTLOrDefault())
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/unlock_failed", "Household Unlock Failed", problemcode.CodeInternalError, "Failed to create the household unlock session", nil)
		return
	}

	setServerCookie(w, &http.Cookie{
		Name:     householdUnlockCookieName,
		Value:    sessionID,
		Path:     "/api/v3/",
		HttpOnly: true,
		Secure:   s.requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, s.currentHouseholdUnlockStatus(true))
}

func (s *Server) DeleteHouseholdUnlock(w http.ResponseWriter, r *http.Request, params DeleteHouseholdUnlockParams) {
	_ = params
	if existingCookie, err := r.Cookie(householdUnlockCookieName); err == nil {
		s.deleteHouseholdUnlock(existingCookie.Value)
	}
	clearServerCookie(w, householdUnlockCookieName, "/api/v3/", s.requestIsHTTPS(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) householdUnlockStoreOrDefault() household.UnlockStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.householdUnlockStore == nil {
		s.householdUnlockStore = household.NewInMemoryUnlockStore()
	}
	return s.householdUnlockStore
}

func (s *Server) householdUnlockTTLOrDefault() time.Duration {
	s.mu.RLock()
	ttl := s.householdUnlockTTL
	s.mu.RUnlock()
	if ttl <= 0 {
		ttl = defaultHouseholdUnlockTTL
	}

	authTTL := s.authSessionTTLOrDefault()
	if authTTL > 0 && ttl > authTTL {
		return authTTL
	}
	return ttl
}

func (s *Server) isHouseholdUnlocked(r *http.Request) bool {
	if r == nil {
		return false
	}
	cookie, err := r.Cookie(householdUnlockCookieName)
	if err != nil {
		return false
	}
	return s.householdUnlockStoreOrDefault().IsUnlocked(cookie.Value)
}

func (s *Server) deleteHouseholdUnlock(sessionID string) {
	s.householdUnlockStoreOrDefault().InvalidateUnlock(sessionID)
}

func (s *Server) currentHouseholdUnlockStatus(unlocked bool) HouseholdUnlockStatus {
	cfg := s.GetConfig()
	return HouseholdUnlockStatus{
		PinConfigured: cfg.Household.PinConfigured(),
		Unlocked:      cfg.Household.PinConfigured() && unlocked,
	}
}

func setServerCookie(w http.ResponseWriter, cookie *http.Cookie) {
	http.SetCookie(w, cookie)
}

func clearServerCookie(w http.ResponseWriter, name, path string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     strings.TrimSpace(name),
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
