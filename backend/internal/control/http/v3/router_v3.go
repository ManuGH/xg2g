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

// RouteRegistrar is the complete external route-registration contract.
type RouteRegistrar interface {
	Register(method, localPattern string, handler http.Handler) error
}

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
	policyRegistrar         RouteRegistrar
	state                   *routeRegistrationState
	missingScopePolicies    map[string]struct{}
	missingExposurePolicies map[string]struct{}
}

type routeRegistrationState struct {
	err error
}

type operationRoute struct {
	Method string
	Path   string
}

func (r routeRegistrar) add(operationID string, handler http.HandlerFunc) {
	if r.state != nil && r.state.err != nil {
		return
	}
	route, ok := operationRoutes[operationID]
	if !ok {
		r.fail(fmt.Errorf("missing generated route for operation %s", operationID))
		return
	}
	scopes, ok := authz.RequiredScopes(operationID)
	if !ok {
		if r.missingScopePolicies != nil {
			r.missingScopePolicies[operationID] = struct{}{}
		}
		if r.router != nil || r.policyRegistrar != nil {
			r.fail(fmt.Errorf("missing scope policy for operation %s", operationID))
		}
		return
	}
	if len(scopes) == 0 && !authz.IsUnscopedAllowed(operationID) {
		if r.router != nil || r.policyRegistrar != nil {
			r.fail(fmt.Errorf("empty scope policy is not allowlisted for operation %s", operationID))
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
		if r.router != nil || r.policyRegistrar != nil {
			r.fail(fmt.Errorf("missing exposure policy for operation %s", operationID))
		}
		return
	}
	if err := authz.ValidateExposurePolicy(operationID, route.Method, scopes, exposure); err != nil {
		if r.router != nil || r.policyRegistrar != nil {
			r.fail(err)
		}
		if r.missingExposurePolicies != nil {
			r.missingExposurePolicies[operationID] = struct{}{}
		}
		return
	}
	if r.router == nil && r.policyRegistrar == nil {
		return
	}
	boundHandler := withRoutePolicy(operationID, scopes, exposure, handler)
	if r.policyRegistrar != nil {
		if err := r.policyRegistrar.Register(route.Method, route.Path, boundHandler); err != nil {
			r.fail(fmt.Errorf("register operation %s: %w", operationID, err))
			return
		}
	}
	if r.router != nil {
		r.router.Method(route.Method, r.baseURL+route.Path, boundHandler)
	}
}

func (r routeRegistrar) fail(err error) {
	if r.state != nil {
		if r.state.err == nil {
			r.state.err = err
		}
		return
	}
	panic(err.Error())
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
