//go:build v3
// +build v3

// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

// NewStateHandler returns an http.Handler that serves GET /api/v3/sessions/{id}.
// The router can be any mux; for MVP we parse the last path element.
func NewStateHandler(st store.StateStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		id := strings.TrimPrefix(r.URL.Path, "/api/v3/sessions/")
		id = strings.Trim(id, "/")
		if id == "" {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		sess, err := st.GetSession(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if sess == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		out := lifecycle.PublicOutcomeFromRecord(sess)
		resp := SessionResponse{
			SessionID:     sess.SessionID,
			ServiceRef:    sess.ServiceRef,
			Profile:       sess.Profile.Name,
			State:         out.State,
			Reason:        out.Reason,
			ReasonDetail:  out.Detail,
			CorrelationID: sess.CorrelationID,
			UpdatedAtMs:   sess.UpdatedAtUnix * 1000,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}
