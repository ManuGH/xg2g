// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net/http"

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
)

// authMiddleware delegates to v3 canonical auth middleware to keep auth behavior
// in one implementation and prevent policy drift between routers.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	if s.v3Handler == nil {
		// Fail closed if v3 subsystem is unavailable.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v3.RespondError(w, r, http.StatusUnauthorized, v3.ErrUnauthorized)
		})
	}
	return s.v3Handler.AuthMiddleware(next)
}
