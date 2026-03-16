// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
)

func writeSessionsDebugResponse(w http.ResponseWriter, result v3sessions.ListSessionsDebugResult) {
	writeJSON(w, http.StatusOK, mapSessionsDebugResponse(result))
}

func mapSessionsDebugResponse(result v3sessions.ListSessionsDebugResult) map[string]any {
	return map[string]any{
		"sessions": result.Sessions,
		"pagination": map[string]int{
			"offset": result.Pagination.Offset,
			"limit":  result.Pagination.Limit,
			"total":  result.Pagination.Total,
			"count":  result.Pagination.Count,
		},
	}
}
