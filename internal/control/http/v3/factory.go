// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/go-chi/chi/v5"
)

// NewHandler creates a V3 API handler with all required middleware wired in.
// It enforces:
// 1. LAN Guard (IP allowlisting)
// 2. Authentication (Via authMiddleware)
// 3. Base URL (/api/v3)
// 4. Request Logging (via middleware.Logger)
//
// This is the canonical way to mount the V3 API.
func NewHandler(svc *Server, cfg config.AppConfig) (http.Handler, error) {
	return newHandlerWithMiddlewares(svc, cfg, nil)
}

func newHandlerWithMiddlewares(svc *Server, cfg config.AppConfig, extra []MiddlewareFunc) (http.Handler, error) {
	// 1. Initialize LAN Guard
	// We rely on config.TrustedProxies to determine trust.
	// We enforce LAN access for the entire V3 API surface by default.
	// Sanitize the trusted proxies list.
	var trustedCIDRs []string
	if cfg.TrustedProxies != "" {
		for _, s := range strings.Split(cfg.TrustedProxies, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				trustedCIDRs = append(trustedCIDRs, s)
			}
		}
	}

	lg, err := middleware.NewLANGuard(middleware.LANGuardConfig{
		TrustedProxyCIDRs: trustedCIDRs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init lan guard: %w", err)
	}

	// 2. Prepare Middleware Stack
	// Order matters:
	// - LANGuard (Reject untrusted IPs first)
	// - Logger (Log allowed requests)
	// - Auth (Verify credentials)
	// - Router (Dispatch)
	stack := []MiddlewareFunc{
		lg.RequireLAN,
		// Simple logger adapter (uses internal/log.Middleware)
		log.Middleware(),
		svc.ScopeMiddlewareFromContext,
		svc.authMiddleware,
	}
	if len(extra) > 0 {
		// Note: server_gen wrapper applies middlewares in order, so the last
		// middleware runs first. Prepending ensures extras run last (post-auth).
		stack = append(extra, stack...)
	}

	// 3. Create Router with RFC 7807 compliant 404/405 handlers
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "system/not_found", "Not Found", "NOT_FOUND", "The requested resource was not found", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusMethodNotAllowed, "system/method_not_allowed", "Method Not Allowed", "METHOD_NOT_ALLOWED", "The requested method is not allowed for this resource", nil)
	})

	// 4. Create Handler
	// Use handwritten router to inject scope policy and keep generated code transport-only.
	h := NewRouter(svc, RouterOptions{
		BaseURL:     V3BaseURL,
		Middlewares: stack,
		BaseRouter:  r,
	})

	return h, nil
}
