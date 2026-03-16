// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
)

func writeSessionsDebugServiceError(w http.ResponseWriter, r *http.Request, err *v3sessions.ListSessionsDebugError) {
	if err == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "unknown sessions debug error")
		return
	}

	switch err.Kind {
	case v3sessions.ListSessionsDebugErrorUnavailable:
		RespondError(w, r, http.StatusServiceUnavailable, ErrV3StoreNotInitialized)
	case v3sessions.ListSessionsDebugErrorInternal:
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
	default:
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
	}
}
