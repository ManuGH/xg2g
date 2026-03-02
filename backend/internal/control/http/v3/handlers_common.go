// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"strconv"
	"strings"

	"reflect"

	"github.com/ManuGH/xg2g/internal/control/http/problem"
)

// ClientProfile represents the detected capability bucket of the client.
type ClientProfile string

const (
	ClientProfileGeneric ClientProfile = "generic"
	ClientProfileSafari  ClientProfile = "safari"
)

// detectClientProfile identifies the client profile from the request.
// Priority:
// 1. Query parameter "profile" (e.g. ?profile=safari)
// 2. Header "X-XG2G-Profile"
// 3. User-Agent sniffing (Fallback)
func detectClientProfile(r *http.Request) ClientProfile {
	// 1. Explicit Query Param
	if p := r.URL.Query().Get("profile"); p != "" {
		return mapProfileString(p)
	}

	// 2. Explicit Header
	if p := r.Header.Get("X-XG2G-Profile"); p != "" {
		return mapProfileString(p)
	}

	// 3. User-Agent Sniffing
	ua := r.UserAgent()
	// Rudimentary check for Safari (excluding Chrome/Android which often contain "Safari")
	if strings.Contains(ua, "Safari") && !strings.Contains(ua, "Chrome") && !strings.Contains(ua, "Android") {
		return ClientProfileSafari
	}

	return ClientProfileGeneric
}

func mapProfileString(s string) ClientProfile {
	switch strings.ToLower(s) {
	case "safari":
		return ClientProfileSafari
	default:
		return ClientProfileGeneric
	}
}

// problemDetailsResponse defines the structure for RFC 7807 responses.
// Note: This shadows the generated ProblemDetails to strictly enforce the "details" extension point
func writeProblem(w http.ResponseWriter, r *http.Request, status int, problemType, title, code, detail string, extra map[string]any) {
	problem.Write(w, r, status, problemType, title, code, detail, extra)
}

// isNil is a robust nil check that handles the "typed nil interface" trap
// for all nillable types (Ptr, Map, Slice, Func, Interface, Chan).
func isNil(i interface{}) bool {
	if i == nil {
		return true
	}
	v := reflect.ValueOf(i)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
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
			limit = val
			if limit > 1000 {
				limit = 1000 // Cap at 1000
			}
		}
	}

	return offset, limit
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
