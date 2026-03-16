// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
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
	ErrUnauthorized  = newRegisteredAPIError(problemcode.CodeUnauthorized, "")
	ErrForbidden     = newRegisteredAPIError(problemcode.CodeForbidden, "")
	ErrInvalidToken  = newRegisteredAPIError(problemcode.CodeInvalidToken, "")
	ErrHTTPSRequired = newRegisteredAPIError(problemcode.CodeHTTPSRequired, "")

	// Resource errors
	ErrBouquetNotFound   = newRegisteredAPIError(problemcode.CodeBouquetNotFound, "")
	ErrRecordingNotFound = newRegisteredAPIError(problemcode.CodeRecordingNotFound, "")
	ErrServiceNotFound   = newRegisteredAPIError(problemcode.CodeServiceNotFound, "")
	ErrFileNotFound      = newRegisteredAPIError(problemcode.CodeFileNotFound, "")

	// Operation errors
	ErrRefreshInProgress       = newRegisteredAPIError(problemcode.CodeRefreshInProgress, "")
	ErrRefreshFailed           = newRegisteredAPIError(problemcode.CodeRefreshFailed, "")
	ErrBreakerOpen             = newRegisteredAPIError(problemcode.CodeBreakerOpen, "")
	ErrAdmissionSessionsFull   = newRegisteredAPIError(problemcode.CodeAdmissionSessionsFull, "")
	ErrAdmissionTranscodesFull = newRegisteredAPIError(problemcode.CodeAdmissionTranscodesFull, "")
	ErrAdmissionNoTuners       = newRegisteredAPIError(problemcode.CodeAdmissionNoTuners, "")
	ErrAdmissionEngineDisabled = newRegisteredAPIError(problemcode.CodeAdmissionEngineDisabled, "")
	ErrAdmissionStateUnknown   = newRegisteredAPIError(problemcode.CodeAdmissionStateUnknown, "")
	ErrTranscodeStartTimeout   = newRegisteredAPIError(problemcode.CodeTranscodeStartTimeout, "")
	ErrTranscodeStalled        = newRegisteredAPIError(problemcode.CodeTranscodeStalled, "")
	ErrTranscodeStalledTimeout = ErrTranscodeStalled
	ErrTranscodeFailed         = newRegisteredAPIError(problemcode.CodeTranscodeFailed, "")
	ErrTranscodeCanceled       = newRegisteredAPIError(problemcode.CodeTranscodeCanceled, "")

	ErrV3Unavailable           = newRegisteredAPIError(problemcode.CodeV3Unavailable, "")
	ErrV3StoreNotInitialized   = newRegisteredAPIError(problemcode.CodeV3Unavailable, "v3 store not initialized")
	ErrV3NotAvailable          = newRegisteredAPIError(problemcode.CodeV3Unavailable, "v3 not available")
	ErrRecordingNotReady       = newRegisteredAPIError(problemcode.CodeRecordingNotReady, "")
	ErrSessionNotFound         = newRegisteredAPIError(problemcode.CodeSessionNotFound, "session not found")
	ErrSessionFeedbackNotFound = newRegisteredAPIError(problemcode.CodeNotFound, "session not found")
	ErrScanUnavailable         = newRegisteredAPIError(problemcode.CodeScanUnavailable, "")

	// Validation errors
	ErrInvalidInput  = newRegisteredAPIError(problemcode.CodeInvalidInput, "")
	ErrPathTraversal = newRegisteredAPIError(problemcode.CodePathTraversal, "")

	// Rate limiting
	ErrRateLimitExceeded        = newRegisteredAPIError(problemcode.CodeRateLimitExceeded, "")
	ErrConcurrentBuildsExceeded = newRegisteredAPIError(problemcode.CodeConcurrentBuildsExceeded, "")

	// Generic errors
	ErrInternalServer      = newRegisteredAPIError(problemcode.CodeInternalServerError, "")
	ErrServiceUnavailable  = newRegisteredAPIError(problemcode.CodeServiceUnavailable, "")
	ErrUpstreamUnavailable = newRegisteredAPIError(problemcode.CodeUpstreamUnavailable, "")
	// CONTRACT-004: Receiver returned result=false
	ErrUpstreamResultFalse = newRegisteredAPIError(problemcode.CodeUpstreamResultFalse, "")

	// Duration parsing errors
	ErrDurationInvalid  = newRegisteredAPIError(problemcode.CodeDurationInvalid, "")
	ErrDurationOverflow = newRegisteredAPIError(problemcode.CodeDurationOverflow, "")
	ErrDurationNegative = newRegisteredAPIError(problemcode.CodeDurationNegative, "")

	// Library errors (Phase 0)
	ErrLibraryScanRunning  = newRegisteredAPIError(problemcode.CodeLibraryScanRunning, "")
	ErrLibraryRootNotFound = newRegisteredAPIError(problemcode.CodeLibraryRootNotFound, "")
)

// RespondError sends a structured error response to the client via writeProblem.
// It automatically extracts the request ID from the context.
func RespondError(w http.ResponseWriter, r *http.Request, statusCode int, apiErr *APIError, details ...any) {
	var d any
	if len(details) > 0 {
		d = details[0]
	}
	spec := problemSpecForAPIError(apiErr, "")

	extra := make(map[string]any)
	if d != nil {
		extra["details"] = d
	}

	// title = Message (human), code = Code (machine), detail = ""
	writeProblem(w, r, statusCode, spec.problemType, spec.title, spec.code, "", extra)
}
