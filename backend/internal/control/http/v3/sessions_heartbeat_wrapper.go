// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type sessionHeartbeatServer interface {
	SessionHeartbeat(w http.ResponseWriter, r *http.Request, sessionID string)
}

// SessionHeartbeat binds POST /sessions/{sessionID}/heartbeat.
func (siw *ServerInterfaceWrapper) SessionHeartbeat(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		siw.ErrorHandlerFunc(w, r, &RequiredParamError{ParamName: "sessionID"})
		return
	}

	heartbeatHandler, ok := siw.Handler.(sessionHeartbeatServer)
	if !ok {
		writeProblem(w, r, http.StatusNotImplemented, "/problems/system/not_implemented", "Not Implemented", "NOT_IMPLEMENTED", "SessionHeartbeat is not implemented", nil)
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heartbeatHandler.SessionHeartbeat(w, r, sessionID)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}
