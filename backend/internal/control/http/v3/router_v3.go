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

// RouterOptions configures the handwritten v3 router.
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

func (r routeRegistrar) add(method, path, operationID string, handler http.HandlerFunc) {
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
	if err := authz.ValidateExposurePolicy(operationID, method, scopes, exposure); err != nil {
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
	r.router.Method(method, r.baseURL+path, withRoutePolicy(operationID, scopes, exposure, handler))
}

// NewRouter registers v3 routes and injects scope policy per operation.
// This replaces generated routing to keep server_gen.go transport-only.
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

	register := routeRegistrar{baseURL: options.BaseURL, router: r}
	registerAllRoutes(register, &wrapper)
	registerOptionalRoutes(register, &pairingRoutesWrapper{
		Handler:            resolvePairingRoutes(si),
		HandlerMiddlewares: options.Middlewares,
	}, &deviceAuthRoutesWrapper{
		Handler:            resolveDeviceAuthRoutes(si),
		HandlerMiddlewares: options.Middlewares,
	})

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

func registerAllRoutes(register routeRegistrar, wrapper *ServerInterfaceWrapper) {
	registerSessionsModuleRoutes(register, wrapper)
	registerRecordingsModuleRoutes(register, wrapper)
	registerDVRModuleRoutes(register, wrapper)
	registerHouseholdModuleRoutes(register, wrapper)
	registerConfigModuleRoutes(register, wrapper)
	registerSystemModuleRoutes(register, wrapper)
}

func registerOptionalRoutes(register routeRegistrar, pairing pairingRoutes, deviceAuth deviceAuthRoutes) {
	registerPairingRoutes(register, pairing)
	registerDeviceAuthRoutes(register, deviceAuth)
}

func missingRouteScopePolicies() []string {
	missing := make(map[string]struct{})
	register := routeRegistrar{missingScopePolicies: missing}
	registerAllRoutes(register, &ServerInterfaceWrapper{})
	registerOptionalRoutes(register, noopPairingRoutes{}, noopDeviceAuthRoutes{})

	out := make([]string, 0, len(missing))
	for operationID := range missing {
		out = append(out, operationID)
	}
	sort.Strings(out)
	return out
}

func missingRouteExposurePolicies() []string {
	missing := make(map[string]struct{})
	register := routeRegistrar{missingExposurePolicies: missing}
	registerAllRoutes(register, &ServerInterfaceWrapper{})
	registerOptionalRoutes(register, noopPairingRoutes{}, noopDeviceAuthRoutes{})

	out := make([]string, 0, len(missing))
	for operationID := range missing {
		out = append(out, operationID)
	}
	sort.Strings(out)
	return out
}

func resolvePairingRoutes(handler ServerInterface) pairingRoutes {
	return serverInterfacePairingAdapter{handler: handler}
}

func resolveDeviceAuthRoutes(handler ServerInterface) deviceAuthRoutes {
	return serverInterfaceDeviceAuthAdapter{handler: handler}
}

type serverInterfacePairingAdapter struct {
	handler ServerInterface
}

func (a serverInterfacePairingAdapter) StartPairing(w http.ResponseWriter, r *http.Request) {
	a.handler.StartPairing(w, r)
}

func (a serverInterfacePairingAdapter) GetPairingStatus(w http.ResponseWriter, r *http.Request) {
	a.handler.GetPairingStatus(w, r, chi.URLParam(r, "pairingId"))
}

func (a serverInterfacePairingAdapter) ApprovePairing(w http.ResponseWriter, r *http.Request) {
	a.handler.ApprovePairing(w, r, chi.URLParam(r, "pairingId"))
}

func (a serverInterfacePairingAdapter) ExchangePairing(w http.ResponseWriter, r *http.Request) {
	a.handler.ExchangePairing(w, r, chi.URLParam(r, "pairingId"))
}

type serverInterfaceDeviceAuthAdapter struct {
	handler ServerInterface
}

func (a serverInterfaceDeviceAuthAdapter) CreateDeviceSession(w http.ResponseWriter, r *http.Request) {
	a.handler.CreateDeviceSession(w, r)
}

func (a serverInterfaceDeviceAuthAdapter) CreateWebBootstrap(w http.ResponseWriter, r *http.Request) {
	a.handler.CreateWebBootstrap(w, r)
}

func (a serverInterfaceDeviceAuthAdapter) CompleteWebBootstrap(w http.ResponseWriter, r *http.Request) {
	a.handler.CompleteWebBootstrap(w, r, chi.URLParam(r, "bootstrapId"), CompleteWebBootstrapParams{
		XXG2GWebBootstrap: r.Header.Get(webBootstrapHeaderName),
	})
}

func defaultBindErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", problemcode.CodeInvalidInput, err.Error(), nil)
}
