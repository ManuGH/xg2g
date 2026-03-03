// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/go-chi/chi/v5"
)

// NewHandler creates a V3 API handler with all required middleware wired in.
// It enforces:
// 1. Authentication (via authMiddleware)
// 2. Scope enforcement (via ScopeMiddlewareFromContext)
// 3. Base URL (/api/v3)
//
// This is the canonical way to mount the V3 API.
func NewHandler(svc *Server, cfg config.AppConfig) (http.Handler, error) {
	return newHandlerWithMiddlewares(svc, cfg, nil)
}

func newHandlerWithMiddlewares(svc *Server, _ config.AppConfig, extra []MiddlewareFunc) (http.Handler, error) {
	// 1. Prepare V3-specific Middleware Stack.
	// Cross-cutting ingress middleware (CORS, security headers, tracing, logging, rate-limit)
	// is applied by the top-level API server stack in internal/api/http.go.
	stack := []MiddlewareFunc{
		svc.ScopeMiddlewareFromContext,
		svc.authMiddleware,
	}
	if len(extra) > 0 {
		// server_gen applies wrapper middlewares in declaration order, where the last
		// middleware becomes outermost. Prepending extras keeps built-in auth outermost.
		stack = append(extra, stack...)
	}

	if missing := missingRouteScopePolicies(); len(missing) > 0 {
		return nil, fmt.Errorf("missing scope policy for operations: %s", strings.Join(missing, ", "))
	}

	// 2. Create Router with RFC 7807 compliant 404/405 handlers
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "system/not_found", "Not Found", "NOT_FOUND", "The requested resource was not found", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusMethodNotAllowed, "system/method_not_allowed", "Method Not Allowed", "METHOD_NOT_ALLOWED", "The requested method is not allowed for this resource", nil)
	})

	// 3. Create Handler
	// Use handwritten router to inject scope policy and keep generated code transport-only.
	h := NewRouter(svc, RouterOptions{
		BaseURL:     V3BaseURL,
		Middlewares: stack,
		BaseRouter:  r,
	})

	return h, nil
}
