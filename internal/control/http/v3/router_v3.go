// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/authz"
	"github.com/go-chi/chi/v5"
)

// RouterOptions configures the handwritten v3 router.
type RouterOptions struct {
	BaseURL          string
	BaseRouter       chi.Router
	Middlewares      []MiddlewareFunc
	ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

type routeRegistrar struct {
	baseURL string
	router  chi.Router
}

func (r routeRegistrar) add(method, path, operationID string, handler http.HandlerFunc) {
	r.router.Method(method, r.baseURL+path, withScopes(operationID, handler))
}

// NewRouter registers v3 routes and injects scope policy per operation.
// This replaces generated routing to keep server_gen.go transport-only.
func NewRouter(si ServerInterface, options RouterOptions) http.Handler {
	r := options.BaseRouter
	if r == nil {
		r = chi.NewRouter()
	}
	if options.ErrorHandlerFunc == nil {
		options.ErrorHandlerFunc = defaultBindErrorHandler
	}

	wrapper := ServerInterfaceWrapper{
		Handler:            si,
		HandlerMiddlewares: options.Middlewares,
		ErrorHandlerFunc:   options.ErrorHandlerFunc,
	}

	register := routeRegistrar{baseURL: options.BaseURL, router: r}
	registerAuthRoutes(register, &wrapper)
	registerDVRRoutes(register, &wrapper)
	registerEPGRoutes(register, &wrapper)
	registerIntentRoutes(register, &wrapper)
	registerLogRoutes(register, &wrapper)
	registerReceiverRoutes(register, &wrapper)
	registerRecordingRoutes(register, &wrapper)
	registerSeriesRoutes(register, &wrapper)
	registerServiceRoutes(register, &wrapper)
	registerSessionRoutes(register, &wrapper)
	registerStreamRoutes(register, &wrapper)
	registerSystemRoutes(register, &wrapper)
	registerTimerRoutes(register, &wrapper)

	return r
}

func withScopes(operationID string, next http.Handler) http.Handler {
	scopes := authz.MustScopes(operationID)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), bearerAuthScopesKey, scopes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func defaultBindErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", "INVALID_INPUT", err.Error(), nil)
}
