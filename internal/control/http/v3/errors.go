// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

// writeJSON writes a JSON response with the given status code.
// If encoding fails, headers are already sent so we can't change the status code,
// but we log the error for debugging.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already sent, can't change status code
		// Log error for debugging - client may receive partial/corrupted response
		log.L().Error().
			Err(err).
			Int("status", code).
			Msg("failed to encode JSON response - client may receive partial data")
	}
}

// ============================================================================
// Structured Error Codes (2025 API Best Practices)
// ============================================================================

// APIError is generated from OpenAPI; provide error interface behavior.
func (e *APIError) Error() string {
	return e.Message
}

// Common API error definitions
var (
	// Authentication/Authorization errors
	ErrUnauthorized = &APIError{
		Code:    "UNAUTHORIZED",
		Message: "Authentication required",
	}
	ErrForbidden = &APIError{
		Code:    "FORBIDDEN",
		Message: "Access denied",
	}
	ErrInvalidToken = &APIError{
		Code:    "INVALID_TOKEN",
		Message: "Invalid or expired API token",
	}

	// Resource errors
	ErrBouquetNotFound = &APIError{
		Code:    "BOUQUET_NOT_FOUND",
		Message: "Bouquet not found",
	}
	ErrRecordingNotFound = &APIError{
		Code:    "RECORDING_NOT_FOUND",
		Message: "Recording not found",
	}
	ErrServiceNotFound = &APIError{
		Code:    "SERVICE_NOT_FOUND",
		Message: "Service not found",
	}
	ErrFileNotFound = &APIError{
		Code:    "FILE_NOT_FOUND",
		Message: "File not found",
	}

	// Operation errors
	ErrRefreshInProgress = &APIError{
		Code:    "REFRESH_IN_PROGRESS",
		Message: "A refresh operation is already in progress",
	}
	ErrRefreshFailed = &APIError{
		Code:    "REFRESH_FAILED",
		Message: "Refresh operation failed",
	}
	ErrCircuitOpen = &APIError{
		Code:    "CIRCUIT_OPEN",
		Message: "Service temporarily unavailable due to repeated failures",
	}
	ErrLeaseBusy = &APIError{
		Code:    "LEASE_BUSY",
		Message: "No tuner available; retry later",
	}
	ErrRecordingNotReady = &APIError{
		Code:    "R_RECORDING_NOT_READY",
		Message: "Recording not ready",
	}

	// Validation errors
	ErrInvalidInput = &APIError{
		Code:    "INVALID_INPUT",
		Message: "Invalid input parameters",
	}
	ErrPathTraversal = &APIError{
		Code:    "PATH_TRAVERSAL",
		Message: "Invalid file path - security violation",
	}

	// Rate limiting
	ErrRateLimitExceeded = &APIError{
		Code:    "RATE_LIMIT_EXCEEDED",
		Message: "Rate limit exceeded - too many requests",
	}
	ErrConcurrentBuildsExceeded = &APIError{
		Code:    "CONCURRENT_BUILDS_EXCEEDED",
		Message: "Too many concurrent recording builds",
	}

	// Generic errors
	ErrInternalServer = &APIError{
		Code:    "INTERNAL_SERVER_ERROR",
		Message: "An internal error occurred",
	}
	ErrServiceUnavailable = &APIError{
		Code:    "SERVICE_UNAVAILABLE",
		Message: "Service temporarily unavailable",
	}
	ErrUpstreamUnavailable = &APIError{
		Code:    "UPSTREAM_UNAVAILABLE",
		Message: "The receiver (OpenWebIF) failed to provide the requested data",
	}
	// CONTRACT-004: Receiver returned result=false
	ErrUpstreamResultFalse = &APIError{
		Code:    "UPSTREAM_RESULT_FALSE",
		Message: "Receiver returned result=false",
	}

	// Duration parsing errors
	ErrDurationInvalid = &APIError{
		Code:    "DURATION_INVALID",
		Message: "Invalid duration format",
	}
	ErrDurationOverflow = &APIError{
		Code:    "DURATION_OVERFLOW",
		Message: "Duration value exceeds maximum allowed limit",
	}
	ErrDurationNegative = &APIError{
		Code:    "DURATION_NEGATIVE",
		Message: "Duration cannot be negative",
	}

	// Library errors (Phase 0)
	ErrLibraryScanRunning = &APIError{
		Code:    "LIBRARY_SCAN_RUNNING",
		Message: "Library scan already in progress, retry later",
	}
	ErrLibraryRootNotFound = &APIError{
		Code:    "LIBRARY_ROOT_NOT_FOUND",
		Message: "Library root not found",
	}
)

// RespondError sends a structured error response to the client via writeProblem.
// It automatically extracts the request ID from the context.
func RespondError(w http.ResponseWriter, r *http.Request, statusCode int, apiErr *APIError, details ...any) {
	var d any
	if len(details) > 0 {
		d = details[0]
	}

	// Map generic APIError to RFC 7807 ProblemDetails
	//   - title: Human-readable short label (APIError.Message)
	//   - code: Stable machine-readable short code (APIError.Code)
	//   - type: Prefixed code for URI reference
	problemType := "error/" + strings.ToLower(apiErr.Code)

	extra := make(map[string]any)
	if d != nil {
		extra["details"] = d
	}

	// title = Message (human), code = Code (machine), detail = ""
	writeProblem(w, r, statusCode, problemType, apiErr.Message, apiErr.Code, "", extra)
}
