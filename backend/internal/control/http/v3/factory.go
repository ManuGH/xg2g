// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/problemcode"
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

// NewHandlerWithRegistrar is a convenience composition of handler construction
// and external route registration.
func NewHandlerWithRegistrar(svc *Server, cfg config.AppConfig, registrar RouteRegistrar) (http.Handler, error) {
	if registrar == nil {
		return nil, fmt.Errorf("RouteRegistrar cannot be nil")
	}
	handler, err := NewHandler(svc, cfg)
	if err != nil {
		return nil, err
	}
	if err := RegisterRoutes(registrar, svc); err != nil {
		return nil, fmt.Errorf("register v3 routes: %w", err)
	}
	return handler, nil
}

// RegisterRoutes registers all generated v3 operations through registrar using
// the same route security stack as NewHandler.
func RegisterRoutes(registrar RouteRegistrar, svc *Server) error {
	if registrar == nil {
		return fmt.Errorf("RouteRegistrar cannot be nil")
	}
	if svc == nil {
		return fmt.Errorf("v3 Server cannot be nil")
	}
	if missing := missingRouteScopePolicies(); len(missing) > 0 {
		return fmt.Errorf("missing scope policy for operations: %s", strings.Join(missing, ", "))
	}
	if missing := missingRouteExposurePolicies(); len(missing) > 0 {
		return fmt.Errorf("missing exposure policy for operations: %s", strings.Join(missing, ", "))
	}

	wrapper := ServerInterfaceWrapper{
		Handler:            svc,
		HandlerMiddlewares: buildV3RouteSecurityStack(svc, nil),
		ErrorHandlerFunc:   defaultBindErrorHandler,
	}
	state := &routeRegistrationState{}
	registerGeneratedRoutes(routeRegistrar{
		policyRegistrar: registrar,
		state:           state,
	}, &wrapper)
	return state.err
}

func buildV3RouteSecurityStack(svc *Server, extra []MiddlewareFunc) []MiddlewareFunc {
	stack := []MiddlewareFunc{
		svc.householdMiddleware,
		svc.ScopeMiddlewareFromContext,
		svc.authMiddleware,
		svc.ExposureSecurityMiddleware,
	}
	if len(extra) > 0 {
		stack = append(append([]MiddlewareFunc{}, extra...), stack...)
	}
	return stack
}

func newHandlerWithMiddlewares(svc *Server, _ config.AppConfig, extra []MiddlewareFunc) (http.Handler, error) {
	// 1. Prepare V3-specific Middleware Stack.
	// Cross-cutting ingress middleware (CORS, security headers, tracing, logging, rate-limit)
	// is applied by the top-level API server stack in internal/api/http.go.
	stack := buildV3RouteSecurityStack(svc, extra)

	if missing := missingRouteScopePolicies(); len(missing) > 0 {
		return nil, fmt.Errorf("missing scope policy for operations: %s", strings.Join(missing, ", "))
	}

	// 2. Create Router with RFC 7807 compliant 404/405 handlers
	r := NewRouteRouter()

	// 3. Create Handler
	// Use handwritten router to inject scope policy and keep generated code transport-only.
	h := NewRouter(svc, RouterOptions{
		BaseURL:     V3BaseURL,
		Middlewares: stack,
		BaseRouter:  r,
	})

	return h, nil
}

// NewRouteRouter creates the configured v3 routing shell used by both the
// internal and externally registered route construction paths.
func NewRouteRouter() chi.Router {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeRegisteredProblem(w, r, http.StatusNotFound, "system/not_found", "Not Found", problemcode.CodeNotFound, "The requested resource was not found", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeRegisteredProblem(w, r, http.StatusMethodNotAllowed, "system/method_not_allowed", "Method Not Allowed", problemcode.CodeMethodNotAllowed, "The requested method is not allowed for this resource", nil)
	})
	return r
}
