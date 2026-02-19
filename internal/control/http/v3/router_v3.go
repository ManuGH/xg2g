// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"net/http"
	"sort"

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
	baseURL         string
	router          chi.Router
	missingPolicies map[string]struct{}
}

func (r routeRegistrar) add(method, path, operationID string, handler http.HandlerFunc) {
	scopes, ok := authz.RequiredScopes(operationID)
	if !ok {
		if r.missingPolicies != nil {
			r.missingPolicies[operationID] = struct{}{}
		}
		if r.router != nil {
			r.router.Method(method, r.baseURL+path, missingScopePolicyHandler(operationID))
		}
		return
	}
	if r.router == nil {
		return
	}
	r.router.Method(method, r.baseURL+path, withScopes(scopes, handler))
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
	registerAllRoutes(register, &wrapper)

	return r
}

func withScopes(scopes []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), bearerAuthScopesKey, scopes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func missingScopePolicyHandler(operationID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusInternalServerError, "system/misconfigured_authz", "Misconfigured Authorization", "MISCONFIGURED_AUTHZ", "Missing scope policy for operation "+operationID, nil)
	})
}

func registerAllRoutes(register routeRegistrar, wrapper *ServerInterfaceWrapper) {
	registerSessionsModuleRoutes(register, wrapper)
	registerRecordingsModuleRoutes(register, wrapper)
	registerDVRModuleRoutes(register, wrapper)
	registerConfigModuleRoutes(register, wrapper)
	registerSystemModuleRoutes(register, wrapper)
}

func missingRouteScopePolicies() []string {
	missing := make(map[string]struct{})
	register := routeRegistrar{missingPolicies: missing}
	registerAllRoutes(register, &ServerInterfaceWrapper{})

	out := make([]string, 0, len(missing))
	for operationID := range missing {
		out = append(out, operationID)
	}
	sort.Strings(out)
	return out
}

func defaultBindErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", "INVALID_INPUT", err.Error(), nil)
}
