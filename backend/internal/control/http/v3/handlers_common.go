// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"strconv"

	"reflect"

	"github.com/ManuGH/xg2g/internal/control/http/problem"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// problemDetailsResponse defines the structure for RFC 7807 responses.
// Note: This shadows the generated ProblemDetails to strictly enforce the "details" extension point
func writeProblem(w http.ResponseWriter, r *http.Request, status int, problemType, title, code, detail string, extra map[string]any) {
	problem.Write(w, r, status, problemType, title, code, detail, extra)
}

func writeRegisteredProblem(w http.ResponseWriter, r *http.Request, status int, problemType, title, code, detail string, extra map[string]any) {
	spec := problemcode.MustResolve(code, title)
	if problemType == "" {
		problemType = spec.ProblemType
	}
	writeProblem(w, r, status, problemType, spec.Title, spec.Code, detail, extra)
}

// isNil is a robust nil check that handles the "typed nil interface" trap
// for all nillable types (Ptr, Map, Slice, Func, Interface, Chan).
func isNil(i any) bool {
	if i == nil {
		return true
	}
	v := reflect.ValueOf(i)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

// parsePaginationParams extracts offset and limit from query parameters.
// Defaults: offset=0, limit=100. Max limit: 1000.
func parsePaginationParams(r *http.Request) (offset int, limit int) {
	// Default values
	offset = 0
	limit = 100

	// Parse offset
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if val, err := strconv.Atoi(offsetStr); err == nil && val >= 0 {
			offset = val
		}
	}

	// Parse limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = min(val,
				// Cap at 1000
				1000)
		}
	}

	return offset, limit
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
