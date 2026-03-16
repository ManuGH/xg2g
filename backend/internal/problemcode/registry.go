// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package problemcode

import (
	"sort"
	"strings"
)

const (
	CodeUnauthorized             = "UNAUTHORIZED"
	CodeForbidden                = "FORBIDDEN"
	CodeInvalidToken             = "INVALID_TOKEN"
	CodeHTTPSRequired            = "HTTPS_REQUIRED"
	CodeCapabilitiesMissing      = "capabilities_missing"
	CodeCapabilitiesInvalid      = "capabilities_invalid"
	CodeDecisionAmbiguous        = "decision_ambiguous"
	CodeInvariantViolation       = "invariant_violation"
	CodeJobConfigInvalid         = "JOB_CONFIG_INVALID"
	CodeJobBouquetsFetchFailed   = "JOB_BOUQUETS_FETCH_FAILED"
	CodeJobBouquetNotFound       = "JOB_BOUQUET_NOT_FOUND"
	CodeJobServicesFetchFailed   = "JOB_SERVICES_FETCH_FAILED"
	CodeJobStreamURLBuildFailed  = "JOB_STREAM_URL_BUILD_FAILED"
	CodeJobPlaylistPathInvalid   = "JOB_PLAYLIST_PATH_INVALID"
	CodeJobPlaylistWriteFailed   = "JOB_PLAYLIST_WRITE_FAILED"
	CodeJobPlaylistWritePerm     = "JOB_PLAYLIST_WRITE_PERMISSION"
	CodeJobXMLTVWriteFailed      = "JOB_XMLTV_WRITE_FAILED"
	CodeJobXMLTVWritePerm        = "JOB_XMLTV_WRITE_PERMISSION"
	CodeJobEPGFetchInvalidInput  = "JOB_EPG_FETCH_INVALID_INPUT"
	CodeJobEPGFetchTimeout       = "JOB_EPG_FETCH_TIMEOUT"
	CodeJobEPGFetchUnavailable   = "JOB_EPG_FETCH_UNAVAILABLE"
	CodeJobEPGFetchFailed        = "JOB_EPG_FETCH_FAILED"
	CodeBouquetNotFound          = "BOUQUET_NOT_FOUND"
	CodeRecordingNotFound        = "RECORDING_NOT_FOUND"
	CodeServiceNotFound          = "SERVICE_NOT_FOUND"
	CodeFileNotFound             = "FILE_NOT_FOUND"
	CodeRefreshInProgress        = "REFRESH_IN_PROGRESS"
	CodeRefreshFailed            = "REFRESH_FAILED"
	CodeBreakerOpen              = "BREAKER_OPEN"
	CodeAdmissionSessionsFull    = "ADMISSION_SESSIONS_FULL"
	CodeAdmissionTranscodesFull  = "ADMISSION_TRANSCODES_FULL"
	CodeAdmissionNoTuners        = "ADMISSION_NO_TUNERS"
	CodeAdmissionEngineDisabled  = "ADMISSION_ENGINE_DISABLED"
	CodeAdmissionStateUnknown    = "ADMISSION_STATE_UNKNOWN"
	CodeTranscodeStartTimeout    = "TRANSCODE_START_TIMEOUT"
	CodeTranscodeStalled         = "TRANSCODE_STALLED"
	CodeTranscodeFailed          = "TRANSCODE_FAILED"
	CodeTranscodeCanceled        = "TRANSCODE_CANCELED"
	CodeV3Unavailable            = "V3_UNAVAILABLE"
	CodeRecordingNotReady        = "R_RECORDING_NOT_READY"
	CodeInvalidInput             = "INVALID_INPUT"
	CodePathTraversal            = "PATH_TRAVERSAL"
	CodeRateLimitExceeded        = "RATE_LIMIT_EXCEEDED"
	CodeConcurrentBuildsExceeded = "CONCURRENT_BUILDS_EXCEEDED"
	CodeInternalServerError      = "INTERNAL_SERVER_ERROR"
	CodeServiceUnavailable       = "SERVICE_UNAVAILABLE"
	CodeUpstreamUnavailable      = "UPSTREAM_UNAVAILABLE"
	CodeUpstreamResultFalse      = "UPSTREAM_RESULT_FALSE"
	CodeDurationInvalid          = "DURATION_INVALID"
	CodeDurationOverflow         = "DURATION_OVERFLOW"
	CodeDurationNegative         = "DURATION_NEGATIVE"
	CodeLibraryScanRunning       = "LIBRARY_SCAN_RUNNING"
	CodeLibraryRootNotFound      = "LIBRARY_ROOT_NOT_FOUND"
	CodeSessionNotFound          = "SESSION_NOT_FOUND"
	CodeNotFound                 = "NOT_FOUND"
	CodeScanUnavailable          = "SCAN_UNAVAILABLE"
	CodeSessionGone              = "session_gone"
	CodeUnavailable              = "UNAVAILABLE"
	CodeInternalError            = "INTERNAL_ERROR"
	CodeSaveFailed               = "SAVE_FAILED"
	CodeReadFailed               = "READ_FAILED"
	CodeMethodNotAllowed         = "METHOD_NOT_ALLOWED"
	CodeNotImplemented           = "NOT_IMPLEMENTED"
	CodeReceiverUnreachable      = "RECEIVER_UNREACHABLE"
	CodeInvalidTime              = "INVALID_TIME"
	CodeInvalidPlaylistPath      = "INVALID_PLAYLIST_PATH"
	CodeConflict                 = "CONFLICT"
	CodeReceiverInconsistent     = "RECEIVER_INCONSISTENT"
	CodeInvalidID                = "INVALID_ID"
	CodeProviderError            = "PROVIDER_ERROR"
	CodeReceiverError            = "RECEIVER_ERROR"
	CodeTokenMissing             = "TOKEN_MISSING"
	CodeTokenMalformed           = "TOKEN_MALFORMED"
	CodeTokenInvalidAlg          = "TOKEN_INVALID_ALG"
	CodeTokenInvalidSig          = "TOKEN_INVALID_SIG"
	CodeTokenExpired             = "TOKEN_EXPIRED"
	CodeTokenNotActive           = "TOKEN_NOT_ACTIVE"
	CodeTokenMissingClaim        = "TOKEN_MISSING_CLAIM"
	CodeTokenIssMismatch         = "TOKEN_ISS_MISMATCH"
	CodeTokenAudMismatch         = "TOKEN_AUD_MISMATCH"
	CodeTokenSubMismatch         = "TOKEN_SUB_MISMATCH"
	CodeTokenModeMismatch        = "TOKEN_MODE_MISMATCH"
	CodeTokenCapMismatch         = "TOKEN_CAP_MISMATCH"
	CodeTokenTTLExceeded         = "TOKEN_TTL_EXCEEDED"
	CodeTokenError               = "TOKEN_ERROR"
	CodeSecurityUnavailable      = "SECURITY_UNAVAILABLE"
	CodeClaimMismatch            = "CLAIM_MISMATCH"
	CodeAdmissionUnavailable     = "ADMISSION_UNAVAILABLE"
	CodeStoreError               = "STORE_ERROR"
	CodeSessionDotNotFound       = "SESSION.NOT_FOUND"
	CodeSessionExpired           = "SESSION.EXPIRED"
	CodeSessionUpdateError       = "SESSION.UPDATE_ERROR"
	CodeDiffFailed               = "DIFF_FAILED"
	CodeEngineError              = "ENGINE_ERROR"
	CodeRecordingPreparing       = "RECORDING_PREPARING"
	CodePreparing                = "PREPARING"
	CodeRemoteProbeUnsupported   = "REMOTE_PROBE_UNSUPPORTED"
	CodeUpstreamError            = "UPSTREAM_ERROR"
	CodeUpstreamAuth             = "UPSTREAM_AUTH"
	CodeUpstreamTimeout          = "UPSTREAM_TIMEOUT"
	CodeInvalidSessionID         = "INVALID_SESSION_ID"
	CodeStopFailed               = "STOP_FAILED"
	CodeInvalidCapabilities      = "INVALID_CAPABILITIES"
	CodePanic                    = "PANIC"
	CodeAddFailed                = "ADD_FAILED"
	CodeDeleteFailed             = "DELETE_FAILED"
	CodeUpdateFailed             = "UPDATE_FAILED"
	CodeClientUnavailable        = "CLIENT_UNAVAILABLE"
	CodeUpstreamEmpty            = "UPSTREAM_EMPTY"
	CodePreflightUnreachable     = "PREFLIGHT_UNREACHABLE"
	CodePreflightTimeout         = "PREFLIGHT_TIMEOUT"
	CodePreflightUnauthorized    = "PREFLIGHT_UNAUTHORIZED"
	CodePreflightForbidden       = "PREFLIGHT_FORBIDDEN"
	CodePreflightNotFound        = "PREFLIGHT_NOT_FOUND"
	CodePreflightBadGateway      = "PREFLIGHT_BAD_GATEWAY"
	CodePreflightInternal        = "PREFLIGHT_INTERNAL"
)

type Spec struct {
	ProblemType  string
	DefaultTitle string
	Description  string
	OperatorHint string
	Severity     Severity
	Retryable    bool
	RunbookURL   string
}

type Entry struct {
	Code         string
	ProblemType  string
	DefaultTitle string
	Description  string
	OperatorHint string
	Severity     Severity
	Retryable    bool
	RunbookURL   string
}

type Resolved struct {
	ProblemType string
	Title       string
	Code        string
}

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

func newRegisteredSpec(code, defaultTitle string) Spec {
	return Spec{
		ProblemType:  "error/" + strings.ToLower(code),
		DefaultTitle: defaultTitle,
		Description:  defaultTitle,
		OperatorHint: "Inspect the related logs and request context for this code, correct the underlying condition, then retry if appropriate.",
		Severity:     SeverityError,
		Retryable:    false,
	}
}

func newCustomProblemSpec(problemType, defaultTitle string) Spec {
	return Spec{
		ProblemType:  problemType,
		DefaultTitle: defaultTitle,
		Description:  defaultTitle,
		OperatorHint: "Inspect the related logs and request context for this code, correct the underlying condition, then retry if appropriate.",
		Severity:     SeverityError,
		Retryable:    false,
	}
}

func specWithDescription(spec Spec, description string) Spec {
	spec.Description = description
	return spec
}

func specWithOperatorHint(spec Spec, operatorHint string) Spec {
	spec.OperatorHint = operatorHint
	return spec
}

func specWithSeverity(spec Spec, severity Severity) Spec {
	spec.Severity = severity
	return spec
}

func specWithRetryable(spec Spec, retryable bool) Spec {
	spec.Retryable = retryable
	return spec
}

func specWithRunbookURL(spec Spec, runbookURL string) Spec {
	spec.RunbookURL = runbookURL
	return spec
}

var registry = map[string]Spec{
	CodeUnauthorized:             specWithRunbookURL(specWithSeverity(specWithOperatorHint(specWithDescription(newRegisteredSpec(CodeUnauthorized, "Authentication required"), "A valid bearer token or session cookie is required before this operation can proceed."), "Verify that the caller sends a valid bearer token or exchange it for a session cookie before retrying the request."), SeverityWarning), "docs/ops/SECURITY.md"),
	CodeForbidden:                specWithRunbookURL(specWithSeverity(specWithOperatorHint(specWithDescription(newRegisteredSpec(CodeForbidden, "Access denied"), "The caller was authenticated, but the granted scopes or claims do not permit this operation."), "Check the token scopes and claims for this caller, then retry with credentials that include the required permission."), SeverityWarning), "docs/ops/SECURITY.md"),
	CodeInvalidToken:             specWithSeverity(newRegisteredSpec(CodeInvalidToken, "Invalid or expired API token"), SeverityWarning),
	CodeHTTPSRequired:            specWithRunbookURL(specWithSeverity(specWithOperatorHint(specWithDescription(newRegisteredSpec(CodeHTTPSRequired, "HTTPS required"), "This operation is accepted only over direct TLS, a trusted HTTPS proxy hop, or the loopback HTTP exception where explicitly supported."), "Retry this operation over HTTPS, or fix the trusted proxy configuration so `X-Forwarded-Proto=https` is presented from an allowed proxy."), SeverityWarning), "docs/ops/SECURITY.md"),
	CodeCapabilitiesMissing:      specWithRunbookURL(specWithSeverity(specWithOperatorHint(specWithDescription(newCustomProblemSpec("recordings/capabilities-missing", "Capabilities Missing"), "The client must provide playback capabilities before the server can choose a safe playback path."), "Collect the client playback capability payload and include it in the playback-info request before retrying."), SeverityWarning), "docs/arch/ADR_PLAYBACK_DECISION.md"),
	CodeCapabilitiesInvalid:      specWithRunbookURL(specWithSeverity(specWithOperatorHint(specWithDescription(newCustomProblemSpec("recordings/capabilities-invalid", "Capabilities Invalid"), "The provided playback capabilities are malformed, unsupported, or inconsistent with the current contract version."), "Validate the capability payload against the current API contract and resubmit it with a supported capabilities version."), SeverityWarning), "docs/arch/ADR_PLAYBACK_DECISION.md"),
	CodeDecisionAmbiguous:        specWithRunbookURL(specWithOperatorHint(specWithDescription(newCustomProblemSpec("recordings/decision-ambiguous", "Decision Ambiguous"), "The server could not derive a single deterministic playback path from the available media truth and client capabilities."), "Inspect media truth and client capabilities together, then retry only after the source profile or capability snapshot is complete and consistent."), "docs/arch/ADR_PLAYBACK_DECISION.md"),
	CodeInvariantViolation:       specWithRunbookURL(specWithSeverity(specWithOperatorHint(specWithDescription(newCustomProblemSpec("recordings/invariant-violation", "Invariant Violation"), "An internal decision-engine invariant failed after a playback decision was computed."), "Capture the request ID and decision inputs from logs, then treat this as a bug and avoid blind retries until the invariant failure is understood."), SeverityCritical), "docs/arch/ADR_P8_DECISION_ENGINE_SEMANTICS.md"),
	CodeJobConfigInvalid:         newRegisteredSpec(CodeJobConfigInvalid, "Job configuration invalid"),
	CodeJobBouquetsFetchFailed:   newRegisteredSpec(CodeJobBouquetsFetchFailed, "Bouquet fetch failed"),
	CodeJobBouquetNotFound:       newRegisteredSpec(CodeJobBouquetNotFound, "Configured bouquet not found"),
	CodeJobServicesFetchFailed:   newRegisteredSpec(CodeJobServicesFetchFailed, "Service fetch failed"),
	CodeJobStreamURLBuildFailed:  newRegisteredSpec(CodeJobStreamURLBuildFailed, "Stream URL build failed"),
	CodeJobPlaylistPathInvalid:   newRegisteredSpec(CodeJobPlaylistPathInvalid, "Playlist path invalid"),
	CodeJobPlaylistWriteFailed:   newRegisteredSpec(CodeJobPlaylistWriteFailed, "Playlist write failed"),
	CodeJobPlaylistWritePerm:     newRegisteredSpec(CodeJobPlaylistWritePerm, "Playlist write permission denied"),
	CodeJobXMLTVWriteFailed:      newRegisteredSpec(CodeJobXMLTVWriteFailed, "XMLTV write failed"),
	CodeJobXMLTVWritePerm:        newRegisteredSpec(CodeJobXMLTVWritePerm, "XMLTV write permission denied"),
	CodeJobEPGFetchInvalidInput:  newRegisteredSpec(CodeJobEPGFetchInvalidInput, "EPG fetch input invalid"),
	CodeJobEPGFetchTimeout:       newRegisteredSpec(CodeJobEPGFetchTimeout, "EPG fetch timed out"),
	CodeJobEPGFetchUnavailable:   newRegisteredSpec(CodeJobEPGFetchUnavailable, "EPG source unavailable"),
	CodeJobEPGFetchFailed:        newRegisteredSpec(CodeJobEPGFetchFailed, "EPG fetch failed"),
	CodeBouquetNotFound:          newRegisteredSpec(CodeBouquetNotFound, "Bouquet not found"),
	CodeRecordingNotFound:        newRegisteredSpec(CodeRecordingNotFound, "Recording not found"),
	CodeServiceNotFound:          newRegisteredSpec(CodeServiceNotFound, "Service not found"),
	CodeFileNotFound:             newRegisteredSpec(CodeFileNotFound, "File not found"),
	CodeRefreshInProgress:        specWithRetryable(specWithSeverity(newRegisteredSpec(CodeRefreshInProgress, "A refresh operation is already in progress"), SeverityInfo), true),
	CodeRefreshFailed:            newRegisteredSpec(CodeRefreshFailed, "Refresh operation failed"),
	CodeBreakerOpen:              specWithRetryable(specWithSeverity(newRegisteredSpec(CodeBreakerOpen, "Service temporarily degraded due to repeated failures"), SeverityWarning), true),
	CodeAdmissionSessionsFull:    specWithRetryable(specWithSeverity(newRegisteredSpec(CodeAdmissionSessionsFull, "Maximum concurrent sessions reached"), SeverityWarning), true),
	CodeAdmissionTranscodesFull:  specWithRetryable(specWithSeverity(newRegisteredSpec(CodeAdmissionTranscodesFull, "Maximum concurrent transcodes reached"), SeverityWarning), true),
	CodeAdmissionNoTuners:        specWithRetryable(specWithSeverity(newRegisteredSpec(CodeAdmissionNoTuners, "No tuners available for this request"), SeverityWarning), true),
	CodeAdmissionEngineDisabled:  newRegisteredSpec(CodeAdmissionEngineDisabled, "Transcode engine is disabled"),
	CodeAdmissionStateUnknown:    specWithRetryable(newRegisteredSpec(CodeAdmissionStateUnknown, "Admission controller state is unknown"), true),
	CodeTranscodeStartTimeout:    specWithRetryable(specWithSeverity(newRegisteredSpec(CodeTranscodeStartTimeout, "Transcode failed to start within time limit"), SeverityWarning), true),
	CodeTranscodeStalled:         specWithRunbookURL(specWithRetryable(specWithOperatorHint(specWithDescription(newRegisteredSpec(CodeTranscodeStalled, "Transcode stalled - no progress detected"), "The FFmpeg watchdog detected that transcoding stopped emitting progress, so the session was terminated as stalled."), "Inspect FFmpeg watchdog and transcode logs for the affected session, verify source health, and restart playback only after the stall cause is resolved."), true), "docs/ops/OBSERVABILITY.md"),
	CodeTranscodeFailed:          newRegisteredSpec(CodeTranscodeFailed, "Transcode process exited with error"),
	CodeTranscodeCanceled:        newRegisteredSpec(CodeTranscodeCanceled, "Transcode was intentionally canceled"),
	CodeV3Unavailable:            newRegisteredSpec(CodeV3Unavailable, "v3 control plane not enabled"),
	CodeRecordingNotReady:        specWithRetryable(specWithSeverity(newRegisteredSpec(CodeRecordingNotReady, "Recording not ready"), SeverityInfo), true),
	CodeInvalidInput:             newRegisteredSpec(CodeInvalidInput, "Invalid input parameters"),
	CodePathTraversal:            newRegisteredSpec(CodePathTraversal, "Invalid file path - security violation"),
	CodeRateLimitExceeded:        newRegisteredSpec(CodeRateLimitExceeded, "Rate limit exceeded - too many requests"),
	CodeConcurrentBuildsExceeded: newRegisteredSpec(CodeConcurrentBuildsExceeded, "Too many concurrent recording builds"),
	CodeInternalServerError:      newRegisteredSpec(CodeInternalServerError, "An internal error occurred"),
	CodeServiceUnavailable:       specWithRetryable(specWithSeverity(newRegisteredSpec(CodeServiceUnavailable, "Service temporarily unavailable"), SeverityWarning), true),
	CodeUpstreamUnavailable:      specWithRetryable(specWithSeverity(newRegisteredSpec(CodeUpstreamUnavailable, "The receiver (OpenWebIF) failed to provide the requested data"), SeverityWarning), true),
	CodeUpstreamResultFalse:      newRegisteredSpec(CodeUpstreamResultFalse, "Receiver returned result=false"),
	CodeDurationInvalid:          newRegisteredSpec(CodeDurationInvalid, "Invalid duration format"),
	CodeDurationOverflow:         newRegisteredSpec(CodeDurationOverflow, "Duration value exceeds maximum allowed limit"),
	CodeDurationNegative:         newRegisteredSpec(CodeDurationNegative, "Duration cannot be negative"),
	CodeLibraryScanRunning:       specWithRetryable(specWithSeverity(newRegisteredSpec(CodeLibraryScanRunning, "Library scan already in progress, retry later"), SeverityInfo), true),
	CodeLibraryRootNotFound:      newRegisteredSpec(CodeLibraryRootNotFound, "Library root not found"),
	CodeSessionNotFound:          specWithSeverity(newRegisteredSpec(CodeSessionNotFound, "session not found"), SeverityInfo),
	CodeNotFound:                 specWithSeverity(newRegisteredSpec(CodeNotFound, "resource not found"), SeverityInfo),
	CodeScanUnavailable:          newRegisteredSpec(CodeScanUnavailable, "Smart Profile Scanner is not initialized"),
	CodeSessionGone:              specWithSeverity(specWithOperatorHint(specWithDescription(newCustomProblemSpec("urn:xg2g:error:session:gone", "Session Gone"), "The requested session is already in a terminal state and can no longer serve live state transitions."), "Create a new playback session instead of retrying operations against the terminated session ID."), SeverityInfo),
	CodeUnavailable:              specWithRetryable(specWithSeverity(newRegisteredSpec(CodeUnavailable, "Service unavailable"), SeverityWarning), true),
	CodeInternalError:            newRegisteredSpec(CodeInternalError, "Internal Error"),
	CodeSaveFailed:               newRegisteredSpec(CodeSaveFailed, "Save Failed"),
	CodeReadFailed:               newRegisteredSpec(CodeReadFailed, "Read Failed"),
	CodeMethodNotAllowed:         newRegisteredSpec(CodeMethodNotAllowed, "Method Not Allowed"),
	CodeNotImplemented:           newRegisteredSpec(CodeNotImplemented, "Not Implemented"),
	CodeReceiverUnreachable:      newRegisteredSpec(CodeReceiverUnreachable, "Receiver Unreachable"),
	CodeInvalidTime:              newRegisteredSpec(CodeInvalidTime, "Invalid Time"),
	CodeInvalidPlaylistPath:      newRegisteredSpec(CodeInvalidPlaylistPath, "Invalid Playlist Path"),
	CodeConflict:                 newRegisteredSpec(CodeConflict, "Conflict"),
	CodeReceiverInconsistent:     newRegisteredSpec(CodeReceiverInconsistent, "Receiver Inconsistent"),
	CodeInvalidID:                newRegisteredSpec(CodeInvalidID, "Invalid ID"),
	CodeProviderError:            newRegisteredSpec(CodeProviderError, "Provider Error"),
	CodeReceiverError:            newRegisteredSpec(CodeReceiverError, "Receiver Error"),
	CodeTokenMissing:             newRegisteredSpec(CodeTokenMissing, "Token Missing"),
	CodeTokenMalformed:           newRegisteredSpec(CodeTokenMalformed, "Token Malformed"),
	CodeTokenInvalidAlg:          newRegisteredSpec(CodeTokenInvalidAlg, "Token Invalid Algorithm"),
	CodeTokenInvalidSig:          newRegisteredSpec(CodeTokenInvalidSig, "Token Invalid Signature"),
	CodeTokenExpired:             newRegisteredSpec(CodeTokenExpired, "Token Expired"),
	CodeTokenNotActive:           newRegisteredSpec(CodeTokenNotActive, "Token Not Active"),
	CodeTokenMissingClaim:        newRegisteredSpec(CodeTokenMissingClaim, "Token Missing Claim"),
	CodeTokenIssMismatch:         newRegisteredSpec(CodeTokenIssMismatch, "Token Issuer Mismatch"),
	CodeTokenAudMismatch:         newRegisteredSpec(CodeTokenAudMismatch, "Token Audience Mismatch"),
	CodeTokenSubMismatch:         newRegisteredSpec(CodeTokenSubMismatch, "Token Subject Mismatch"),
	CodeTokenModeMismatch:        newRegisteredSpec(CodeTokenModeMismatch, "Token Mode Mismatch"),
	CodeTokenCapMismatch:         newRegisteredSpec(CodeTokenCapMismatch, "Token Capabilities Mismatch"),
	CodeTokenTTLExceeded:         newRegisteredSpec(CodeTokenTTLExceeded, "Token TTL Exceeded"),
	CodeTokenError:               newRegisteredSpec(CodeTokenError, "Token Error"),
	CodeSecurityUnavailable:      newRegisteredSpec(CodeSecurityUnavailable, "Security Unavailable"),
	CodeClaimMismatch:            newRegisteredSpec(CodeClaimMismatch, "Claim Mismatch"),
	CodeAdmissionUnavailable:     newRegisteredSpec(CodeAdmissionUnavailable, "Admission Unavailable"),
	CodeStoreError:               newRegisteredSpec(CodeStoreError, "Store Error"),
	CodeSessionDotNotFound:       newRegisteredSpec(CodeSessionDotNotFound, "Session Not Found"),
	CodeSessionExpired:           specWithSeverity(newRegisteredSpec(CodeSessionExpired, "Session Expired"), SeverityWarning),
	CodeSessionUpdateError:       newRegisteredSpec(CodeSessionUpdateError, "Session Update Error"),
	CodeDiffFailed:               newRegisteredSpec(CodeDiffFailed, "Diff Failed"),
	CodeEngineError:              newRegisteredSpec(CodeEngineError, "Engine Error"),
	CodeRecordingPreparing:       specWithRetryable(specWithSeverity(newRegisteredSpec(CodeRecordingPreparing, "Media is being analyzed"), SeverityInfo), true),
	CodePreparing:                specWithRetryable(specWithSeverity(newRegisteredSpec(CodePreparing, "Preparing"), SeverityInfo), true),
	CodeRemoteProbeUnsupported:   newRegisteredSpec(CodeRemoteProbeUnsupported, "Remote Probe Unsupported"),
	CodeUpstreamError:            newRegisteredSpec(CodeUpstreamError, "Upstream Error"),
	CodeUpstreamAuth:             newRegisteredSpec(CodeUpstreamAuth, "Upstream Auth Failed"),
	CodeUpstreamTimeout:          specWithRetryable(specWithSeverity(newRegisteredSpec(CodeUpstreamTimeout, "Upstream Timeout"), SeverityWarning), true),
	CodeInvalidSessionID:         newRegisteredSpec(CodeInvalidSessionID, "Invalid Session ID"),
	CodeStopFailed:               newRegisteredSpec(CodeStopFailed, "Stop Failed"),
	CodeInvalidCapabilities:      newRegisteredSpec(CodeInvalidCapabilities, "Invalid Capabilities"),
	CodePanic:                    specWithSeverity(newRegisteredSpec(CodePanic, "Internal Server Error"), SeverityCritical),
	CodeAddFailed:                newRegisteredSpec(CodeAddFailed, "Add Failed"),
	CodeDeleteFailed:             newRegisteredSpec(CodeDeleteFailed, "Delete Failed"),
	CodeUpdateFailed:             newRegisteredSpec(CodeUpdateFailed, "Update Failed"),
	CodeClientUnavailable:        specWithRetryable(specWithSeverity(newRegisteredSpec(CodeClientUnavailable, "Client Unavailable"), SeverityWarning), true),
	CodeUpstreamEmpty:            newRegisteredSpec(CodeUpstreamEmpty, "Empty Upstream Response"),
	CodePreflightUnreachable:     specWithRunbookURL(specWithRetryable(specWithSeverity(newCustomProblemSpec("preflight/unreachable", "Source unreachable"), SeverityWarning), true), "docs/ops/PREFLIGHT.md"),
	CodePreflightTimeout:         specWithRunbookURL(specWithRetryable(specWithSeverity(newCustomProblemSpec("preflight/timeout", "Source timeout"), SeverityWarning), true), "docs/ops/PREFLIGHT.md"),
	CodePreflightUnauthorized:    specWithRunbookURL(specWithSeverity(newCustomProblemSpec("preflight/unauthorized", "Unauthorized"), SeverityWarning), "docs/ops/PREFLIGHT.md"),
	CodePreflightForbidden:       specWithRunbookURL(specWithSeverity(newCustomProblemSpec("preflight/forbidden", "Forbidden"), SeverityWarning), "docs/ops/PREFLIGHT.md"),
	CodePreflightNotFound:        specWithRunbookURL(specWithSeverity(newCustomProblemSpec("preflight/not_found", "Not found"), SeverityInfo), "docs/ops/PREFLIGHT.md"),
	CodePreflightBadGateway:      specWithRunbookURL(specWithRetryable(specWithSeverity(newCustomProblemSpec("preflight/bad_gateway", "Bad gateway"), SeverityWarning), true), "docs/ops/PREFLIGHT.md"),
	CodePreflightInternal:        specWithRunbookURL(specWithRetryable(specWithSeverity(newCustomProblemSpec("preflight/internal", "Internal error"), SeverityError), true), "docs/ops/PREFLIGHT.md"),
}

var privateCodes = map[string]struct{}{
	CodeJobConfigInvalid:        {},
	CodeJobBouquetsFetchFailed:  {},
	CodeJobBouquetNotFound:      {},
	CodeJobServicesFetchFailed:  {},
	CodeJobStreamURLBuildFailed: {},
	CodeJobPlaylistPathInvalid:  {},
	CodeJobPlaylistWriteFailed:  {},
	CodeJobPlaylistWritePerm:    {},
	CodeJobXMLTVWriteFailed:     {},
	CodeJobXMLTVWritePerm:       {},
	CodeJobEPGFetchInvalidInput: {},
	CodeJobEPGFetchTimeout:      {},
	CodeJobEPGFetchUnavailable:  {},
	CodeJobEPGFetchFailed:       {},
}

func Entries() []Entry {
	entries := make([]Entry, 0, len(registry))
	for code, spec := range registry {
		entries = append(entries, Entry{
			Code:         code,
			ProblemType:  spec.ProblemType,
			DefaultTitle: spec.DefaultTitle,
			Description:  spec.Description,
			OperatorHint: spec.OperatorHint,
			Severity:     spec.Severity,
			Retryable:    spec.Retryable,
			RunbookURL:   spec.RunbookURL,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Code < entries[j].Code
	})
	return entries
}

func PublicEntries() []Entry {
	entries := make([]Entry, 0, len(registry)-len(privateCodes))
	for _, entry := range Entries() {
		if _, private := privateCodes[entry.Code]; private {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func Lookup(code string) (Spec, bool) {
	spec, ok := registry[code]
	return spec, ok
}

func MustLookup(code string) Spec {
	spec, ok := Lookup(code)
	if !ok {
		panic("missing problem code registry entry for " + code)
	}
	return spec
}

func Resolve(code, title string) Resolved {
	spec, ok := Lookup(code)
	if !ok {
		if title == "" {
			title = code
		}
		return Resolved{
			ProblemType: "error/" + strings.ToLower(code),
			Title:       title,
			Code:        code,
		}
	}
	if title == "" {
		title = spec.DefaultTitle
	}
	return Resolved{
		ProblemType: spec.ProblemType,
		Title:       title,
		Code:        code,
	}
}

func MustResolve(code, title string) Resolved {
	spec := MustLookup(code)
	if title == "" {
		title = spec.DefaultTitle
	}
	return Resolved{
		ProblemType: spec.ProblemType,
		Title:       title,
		Code:        code,
	}
}
