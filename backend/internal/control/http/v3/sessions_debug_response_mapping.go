// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
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
			if sCopy.ContextData != nil {
				cdCopy := make(map[string]string, len(sCopy.ContextData))
				for k, v := range sCopy.ContextData {
					if k == model.CtxKeySource || isIPTVRef(v) {
						cdCopy[k] = maskFn(v)
					} else if k == model.CtxKeyRuntimePolicyReplay {
						var replay runtimepolicy.RuntimePolicyReplay
						if err := json.Unmarshal([]byte(v), &replay); err == nil {
							if replay.Metadata.ServiceRef != "" {
								replay.Metadata.ServiceRef = maskFn(replay.Metadata.ServiceRef)
							}
							if b, err := json.Marshal(replay); err == nil {
								cdCopy[k] = string(b)
								continue
							}
						}
						cdCopy[k] = v
					} else {
						cdCopy[k] = v
					}
				}
				sCopy.ContextData = cdCopy
			}
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
