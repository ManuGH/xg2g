// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// corsAllowedHeaders / corsExposedHeaders are the request and response header
// surfaces advertised to allowed cross-origin callers.
const (
	corsAllowedHeaders = "Content-Type, X-Request-ID, X-API-Token, Authorization"
	corsExposedHeaders = "Retry-After, Content-Length, Date, X-Request-ID"
	corsMaxAgeSeconds  = "600"
)

// fallbackPreflightMethods is the method list used when the preflight responder
// has no router to ask. It is deliberately the last resort: a route-aware
// preflight (see Preflight) advertises only the methods a path really accepts.
var fallbackPreflightMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodOptions,
	http.MethodDelete, http.MethodPut, http.MethodPatch,
}

// preflightProbeMethods are the methods probed against the router to compute a
// path's true Allow set.
var preflightProbeMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost,
	http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// RouteMatcher is the slice of chi.Mux the preflight responder needs to discover
// which methods a path actually accepts. *chi.Mux satisfies it.
type RouteMatcher interface {
	Match(rctx *chi.Context, method, path string) bool
}

// CORSHeaders returns a middleware that stamps Cross-Origin Resource Sharing
// response headers. It never short-circuits — answering preflights is the job of
// Preflight, which runs later in the stack.
//
// Splitting header-stamping from preflight-answering is what lets a rate-limit
// rejection still carry correct CORS headers. If CORS answered and returned in
// one step, any middleware after it that produced a 429 would emit a response
// the browser could only report as an opaque network error, hiding the real
// cause from the UI.
func CORSHeaders(allowedOrigins []string, allowCredentials bool) func(http.Handler) http.Handler {
	// Create map for O(1) lookup
	allowed := make(map[string]bool)
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Special case: "*" in configuration allows all origins.
			allowAll := allowed["*"]

			// Only emit CORS response headers when the origin is actually allowed.
			// Access-Control-Allow-Headers/Expose-Headers/Max-Age are only
			// meaningful alongside an allowed Allow-Origin; emitting them for
			// disallowed or origin-less callers leaks the API's header surface.
			if origin != "" && (allowAll || allowed[origin]) {
				if allowAll {
					// Wildcard: emit * instead of reflecting the request Origin.
					// Per the Fetch spec, Access-Control-Allow-Origin: * cannot carry
					// Access-Control-Allow-Credentials: true.  When allowCredentials is
					// true alongside a wildcard origin list, we suppress the credentials
					// header — reflecting the actual origin would allow any arbitrary
					// website to make credentialed cross-origin requests, which is a
					// classic CORS misconfiguration.
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if allowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
				w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
				w.Header().Set("Access-Control-Expose-Headers", corsExposedHeaders)
				w.Header().Set("Access-Control-Max-Age", corsMaxAgeSeconds)
			}

			// Always set Vary: Origin to prevent cache poisoning/confusion
			vary := w.Header().Get("Vary")
			if vary == "" {
				w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
			} else if !strings.Contains(vary, "Origin") {
				w.Header().Set("Vary", vary+", Origin")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Preflight answers CORS preflight (OPTIONS) requests.
//
// When given a router it asks which methods the requested path actually accepts
// and advertises exactly those, instead of a blanket list identical for every
// URL. Two consequences:
//
//   - A path that accepts only GET no longer claims to accept DELETE and PATCH.
//   - A path the router does not know at all falls through to the router's own
//     NotFound handling, so an unknown URL does not get a success-shaped 204 the
//     way a blanket preflight responder would hand out.
//
// Access-Control-Allow-Methods is emitted only when CORSHeaders already accepted
// the origin. A disallowed origin therefore gets a 204 with no method list,
// which is exactly what makes the browser fail the preflight — without ever
// disclosing the method surface.
func Preflight(router RouteMatcher) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			methods := preflightAllowedMethods(router, r.URL.Path)
			if len(methods) == 0 {
				// Unknown route: let the router answer (404) rather than
				// fabricating a successful preflight.
				next.ServeHTTP(w, r)
				return
			}

			allow := strings.Join(methods, ", ")
			w.Header().Set("Allow", allow)
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				w.Header().Set("Access-Control-Allow-Methods", allow)
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

// preflightAllowedMethods reports the methods path accepts. With no router it
// returns the static fallback list; a nil result means "route unknown".
//
// Probing cannot see through a sub-router: a path mounted as a catch-all (chi's
// Handle registers every method) reports the full set, so paths under such a
// mount still get a broad list. Narrowing those would mean binding a preflight
// responder inside the sub-router as well. Routes registered directly on this
// router — health, static, operator endpoints — do get an exact list.
//
// An explicitly registered OPTIONS route is deliberately NOT detected and
// skipped here: chi's Handle registers OPTIONS alongside every other method, so
// such a check cannot tell a deliberate r.Options() from an ordinary catch-all
// mount, and it fired on every mounted subtree. Preflights are therefore
// answered here in all cases — the same thing the previous CORS middleware did
// unconditionally, so nothing that used to reach a handler stops reaching it.
func preflightAllowedMethods(router RouteMatcher, path string) []string {
	if router == nil {
		return fallbackPreflightMethods
	}

	methods := make([]string, 0, len(preflightProbeMethods)+1)
	for _, m := range preflightProbeMethods {
		// chi mutates the RouteContext while matching, so each probe gets a
		// fresh one and the request's real routing context stays untouched.
		if router.Match(chi.NewRouteContext(), m, path) {
			methods = append(methods, m)
		}
	}
	if len(methods) == 0 {
		return nil
	}
	return append(methods, http.MethodOptions)
}

// CORS wires header-stamping and preflight-answering together with no router
// binding. Retained for callers (and tests) that want CORS as a single
// middleware; the server stack composes the two halves itself so it can slot a
// preflight rate limiter between them.
func CORS(allowedOrigins []string, allowCredentials bool) func(http.Handler) http.Handler {
	headers := CORSHeaders(allowedOrigins, allowCredentials)
	preflight := Preflight(nil)
	return func(next http.Handler) http.Handler {
		return headers(preflight(next))
	}
}
