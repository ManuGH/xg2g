// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package system

import "net/http"

// HealthResponder captures the health/readiness contract used by HTTP handlers.
type HealthResponder interface {
	ServeHealth(http.ResponseWriter, *http.Request)
	ServeReady(http.ResponseWriter, *http.Request)
}

// NewHealthHandler returns a handler for /healthz.
func NewHealthHandler(responder HealthResponder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if responder == nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		responder.ServeHealth(w, r)
	}
}

// NewReadyHandler returns a handler for /readyz.
func NewReadyHandler(responder HealthResponder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if responder == nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		responder.ServeReady(w, r)
	}
}
