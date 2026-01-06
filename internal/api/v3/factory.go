// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/api/middleware"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
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
		svc.authMiddleware,
	}

	// 3. Create Handler
	// We use the generated HandlerWithOptions to apply the BaseURL and our middleware stack.
	h := HandlerWithOptions(svc, ChiServerOptions{
		BaseURL:     "/api/v3",
		Middlewares: stack,
	})

	return h, nil
}
