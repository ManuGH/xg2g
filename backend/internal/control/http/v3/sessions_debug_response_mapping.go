// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func writeSessionsDebugResponse(w http.ResponseWriter, result v3sessions.ListSessionsDebugResult, maskFns ...func(string) string) {
	writeJSON(w, http.StatusOK, mapSessionsDebugResponse(result, maskFns...))
}

func mapSessionsDebugResponse(result v3sessions.ListSessionsDebugResult, maskFns ...func(string) string) map[string]any {
	var maskFn func(string) string
	if len(maskFns) > 0 {
		maskFn = maskFns[0]
	}

	sessions := result.Sessions
	if maskFn != nil && len(sessions) > 0 {
		masked := make([]*model.SessionRecord, len(sessions))
		for i, sess := range sessions {
			if sess == nil {
				continue
			}
			sCopy := *sess
			sCopy.ServiceRef = maskFn(sCopy.ServiceRef)
			masked[i] = &sCopy
		}
		sessions = masked
	}

	return map[string]any{
		"sessions": sessions,
		"pagination": map[string]int{
			"offset": result.Pagination.Offset,
			"limit":  result.Pagination.Limit,
			"total":  result.Pagination.Total,
			"count":  result.Pagination.Count,
		},
	}
}
