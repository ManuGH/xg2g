package api

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/log"
)

// handleSessionLogin issues a secure, HttpOnly session cookie for the authenticated user.
// This allows native players (e.g. Safari HLS) to access protected streams without leaking tokens in the URL.
// The endpoint itself is protected by authMiddleware (Bearer token required).
func (s *Server) handleSessionLogin(w http.ResponseWriter, r *http.Request) {
	// The request is already authenticated by middleware, so we trust the caller has the correct token.
	token := s.cfg.APIToken
	if token == "" {
		// If auth is disabled, just return OK (cookie not needed)
		w.WriteHeader(http.StatusOK)
		return
	}

	logger := log.WithComponentFromContext(r.Context(), "auth")
	logger.Info().Str("event", "session.create").Str("remote_addr", r.RemoteAddr).Msg("issuing stream session cookie")

	// Set a cookie containing the API token (or a signed session equivalent).
	// Since XG2G is stateless, and the API Token is the only secret, we use it directly.
	// It is protected by HttpOnly and SameSite=Strict constraints.
	http.SetCookie(w, &http.Cookie{
		Name:     "xg2g_session",
		Value:    token,
		Path:     "/",          // Must be root to match /stream/ paths
		HttpOnly: true,         // Prevent JS access (XSS protection)
		Secure:   r.TLS != nil, // Auto-detect TLS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24 hours (long enough for extended viewing)
	})

	w.WriteHeader(http.StatusOK)
}
