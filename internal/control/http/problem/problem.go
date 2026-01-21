package problem

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/log"
)

// Write writes an RFC 7807 problem details response.
//
// Semantics:
//   - type: Canonical machine identifier (e.g. "system/not_found").
//   - title: Human-readable short label (e.g. "Not Found").
//   - code: Stable machine-readable short code (e.g. "NOT_FOUND").
//   - detail: Human-readable explanation of the specific error.
//
// Legacy clients/tests:
//   - The "code" field is included for both machine-readable logic and backward compatibility.
func Write(w http.ResponseWriter, r *http.Request, status int, problemType, title, code, detail string, extra map[string]any) {
	if r == nil {
		// V3 Invariant: All handlers must pass the request to the error writer.
		// If this happens in production, it's a developer error.
		log.L().Error().Str("type", problemType).Int("status", status).Msg("problem.Write called with nil request")
	}

	instance := ""
	if r != nil {
		instance = r.URL.EscapedPath()
	}

	// 1. Header-Truth: Request ID from context or response header (canonical)
	reqID := ""
	if r != nil {
		reqID = log.RequestIDFromContext(r.Context())
	}
	if reqID == "" {
		reqID = w.Header().Get(HeaderRequestID)
	}
	if reqID == "" {
		// Stop-the-line: Every V3 response must have a Request ID.
		// If we reach here, the middleware or context propagation failed.
		reqID = "FALLBACK-TRUTH-MISSING"
	}

	// 2. Build the response map (RFC 7807 compatible)
	res := map[string]any{
		"type":           problemType,
		"title":          title,
		"status":         status,
		"code":           code,
		JSONKeyRequestID: reqID,
	}

	if detail != "" {
		res["detail"] = detail
	}
	if instance != "" {
		res["instance"] = instance
	}

	// Add extensions (Extras) at top level, protecting reserved keys.
	for k, v := range extra {
		switch k {
		case "type", "title", "status", "detail", "instance", "code":
			log.L().Warn().Str("key", k).Str("problem_type", problemType).Msg("ignoring reserved key in problem extras")
			continue
		}
		res[k] = v
	}

	w.Header().Set(HeaderRequestID, reqID)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(res); err != nil {
		log.L().Error().
			Err(err).
			Str("type", problemType).
			Int("status", status).
			Msg("failed to encode problem response")
	}
}
