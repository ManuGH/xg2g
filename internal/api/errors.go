// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// writeJSON writes a JSON response with the given status code
//
//nolint:unused // Legacy function - kept for future use
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a generic error response
//
//nolint:unused // Legacy function - kept for future use
func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

// writeUnauthorized writes a 401 Unauthorized response
//
//nolint:unused // Legacy function - kept for future use
func writeUnauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

// writeForbidden writes a 403 Forbidden response
//
//nolint:unused // Legacy function - kept for future use
func writeForbidden(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
}

// writeNotFound writes a 404 Not Found response
//
//nolint:unused // Legacy function - kept for future use
func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// writeServiceUnavailable writes a 503 Service Unavailable response
//
//nolint:unused // Legacy function - kept for future use
func writeServiceUnavailable(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
}

// setDownloadHeaders sets appropriate headers for file downloads
//
//nolint:unused // Legacy function - kept for future use
func setDownloadHeaders(w http.ResponseWriter, name string, size int64, mod time.Time) {
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Last-Modified", mod.UTC().Format(http.TimeFormat))

	// Set Content-Type based on file extension
	switch {
	case strings.HasSuffix(name, ".m3u"), strings.HasSuffix(name, ".m3u8"):
		w.Header().Set("Content-Type", "audio/x-mpegurl")
	case strings.HasSuffix(name, ".xml"):
		w.Header().Set("Content-Type", "application/xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
}

// ============================================================================
// Structured Error Codes (2025 API Best Practices)
// ============================================================================

// APIError represents a structured error response for the API.
// It provides machine-readable error codes and human-friendly messages.
type APIError struct {
	Code      string `json:"code"`              // Machine-readable error code
	Message   string `json:"message"`           // Human-readable error message
	RequestID string `json:"request_id"`        // Request ID for support/debugging
	Details   any    `json:"details,omitempty"` // Optional additional context
}

// Error implements the error interface
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

	// Generic errors
	ErrInternalServer = &APIError{
		Code:    "INTERNAL_SERVER_ERROR",
		Message: "An internal error occurred",
	}
	ErrServiceUnavailable = &APIError{
		Code:    "SERVICE_UNAVAILABLE",
		Message: "Service temporarily unavailable",
	}
)

// RespondError sends a structured error response to the client.
// It automatically extracts the request ID from the context.
func RespondError(w http.ResponseWriter, r *http.Request, statusCode int, apiErr *APIError, details ...any) {
	// Clone the error to avoid modifying the original
	response := &APIError{
		Code:      apiErr.Code,
		Message:   apiErr.Message,
		RequestID: log.RequestIDFromContext(r.Context()),
	}

	// Add optional details if provided
	if len(details) > 0 {
		response.Details = details[0]
	}

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Encode response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		// Fallback if JSON encoding fails
		http.Error(w, apiErr.Message, statusCode)
	}
}
