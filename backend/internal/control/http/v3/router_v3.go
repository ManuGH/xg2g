// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/authz"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/go-chi/chi/v5"
)

// RouterOptions configures the policy-aware v3 router.
type RouterOptions struct {
	BaseURL          string
	BaseRouter       chi.Router
	Middlewares      []MiddlewareFunc
	ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

type routeRegistrar struct {
	baseURL                 string
	router                  chi.Router
	missingScopePolicies    map[string]struct{}
	missingExposurePolicies map[string]struct{}
}

type operationRoute struct {
	Method string
	Path   string
}

func (r routeRegistrar) add(operationID string, handler http.HandlerFunc) {
	route, ok := operationRoutes[operationID]
	if !ok {
		panic(fmt.Sprintf("missing generated route for operation %s", operationID))
	}
	scopes, ok := authz.RequiredScopes(operationID)
	if !ok {
		if r.missingScopePolicies != nil {
			r.missingScopePolicies[operationID] = struct{}{}
		}
		if r.router != nil {
			panic(fmt.Sprintf("missing scope policy for operation %s", operationID))
		}
		return
	}
	if len(scopes) == 0 && !authz.IsUnscopedAllowed(operationID) {
		if r.router != nil {
			panic(fmt.Sprintf("empty scope policy is not allowlisted for operation %s", operationID))
		}
		if r.missingScopePolicies != nil {
			r.missingScopePolicies[operationID] = struct{}{}
		}
		return
	}
	exposure, ok := authz.ExposurePolicyForOperation(operationID)
	if !ok {
		if r.missingExposurePolicies != nil {
			r.missingExposurePolicies[operationID] = struct{}{}
		}
		if r.router != nil {
			panic(fmt.Sprintf("missing exposure policy for operation %s", operationID))
		}
		return
	}
	if err := authz.ValidateExposurePolicy(operationID, route.Method, scopes, exposure); err != nil {
		if r.router != nil {
			panic(err.Error())
		}
		if r.missingExposurePolicies != nil {
			r.missingExposurePolicies[operationID] = struct{}{}
		}
		return
	}
	if r.router == nil {
		return
	}
	r.router.Method(route.Method, r.baseURL+route.Path, withRoutePolicy(operationID, scopes, exposure, handler))
}

// NewRouter mounts the generated operation catalog and injects its policy per route.
func NewRouter(si ServerInterface, options RouterOptions) http.Handler {
	if missing := missingRouteScopePolicies(); len(missing) > 0 {
		panic("missing scope policy for operations: " + strings.Join(missing, ", "))
	}
	if missing := missingRouteExposurePolicies(); len(missing) > 0 {
		panic("missing exposure policy for operations: " + strings.Join(missing, ", "))
	}

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

	registerGeneratedRoutes(routeRegistrar{baseURL: options.BaseURL, router: r}, &wrapper)

	return r
}

func withRoutePolicy(operationID string, scopes []string, exposure authz.ExposurePolicy, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), bearerAuthScopesKey, scopes)
		ctx = context.WithValue(ctx, operationIDKey, operationID)
		ctx = context.WithValue(ctx, exposurePolicyKey, exposure)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func missingRouteScopePolicies() []string {
	missing := make(map[string]struct{})
	registerGeneratedRoutes(routeRegistrar{missingScopePolicies: missing}, &ServerInterfaceWrapper{})

	out := make([]string, 0, len(missing))
	for operationID := range missing {
		out = append(out, operationID)
	}
	sort.Strings(out)
	return out
}

func missingRouteExposurePolicies() []string {
	missing := make(map[string]struct{})
	registerGeneratedRoutes(routeRegistrar{missingExposurePolicies: missing}, &ServerInterfaceWrapper{})

	out := make([]string, 0, len(missing))
	for operationID := range missing {
		out = append(out, operationID)
	}
	sort.Strings(out)
	return out
}

func defaultBindErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, err.Error(), nil)
}
