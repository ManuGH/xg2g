package diagnostics

// Error codes per ADR-SRE-002 error taxonomy.

// Receiver Layer Error Codes
const (
	ErrReceiverUnreachable = "RECEIVER_UNREACHABLE" // Network timeout or connection refused
	ErrReceiverTimeout     = "RECEIVER_TIMEOUT"     // Response took > 5s
	ErrReceiverHTTPError   = "RECEIVER_HTTP_ERROR"  // HTTP 4xx/5xx
)

// DVR Layer Error Codes
const (
	ErrUpstreamResultFalse = "UPSTREAM_RESULT_FALSE" // OpenWebIF returned result: false
	ErrUpstreamTimeout     = "UPSTREAM_TIMEOUT"      // OpenWebIF didn't respond within deadline
	ErrUpstreamParseError  = "UPSTREAM_PARSE_ERROR"  // Malformed response
)

// EPG Layer Error Codes
const (
	ErrEPGEmpty      = "EPG_EMPTY"       // No EPG data available
	ErrEPGPartial    = "EPG_PARTIAL"     // Only some channels have EPG
	ErrEPGTimeout    = "EPG_TIMEOUT"     // EPG endpoint timeout
	ErrEPGParseError = "EPG_PARSE_ERROR" // Malformed EPG response
)

// Library Layer Error Codes
const (
	ErrRootPathNotFound     = "ROOT_PATH_NOT_FOUND"    // Configured path doesn't exist
	ErrRootPermissionDenied = "ROOT_PERMISSION_DENIED" // Cannot read root directory
	ErrScanPartialFailure   = "SCAN_PARTIAL_FAILURE"   // Some subdirs unreadable
	ErrScanTimeout          = "SCAN_TIMEOUT"           // Scan exceeded budget
)

// Playback Layer Error Codes
const (
	ErrFFmpegNotFound = "FFMPEG_NOT_FOUND" // FFmpeg binary missing
	ErrLeaseExhausted = "LEASE_EXHAUSTED"  // All tuner slots in use
	ErrSessionCrashed = "SESSION_CRASHED"  // FFmpeg exited unexpectedly
)

// User-facing error messages per ADR-SRE-002.
var ErrorMessages = map[string]string{
	// Receiver
	ErrReceiverUnreachable: "Receiver offline or unreachable",
	ErrReceiverTimeout:     "Receiver responding slowly",
	ErrReceiverHTTPError:   "Receiver returned error",

	// DVR
	ErrUpstreamResultFalse: "Recording list unavailable from receiver",
	ErrUpstreamTimeout:     "Recording list timeout",
	ErrUpstreamParseError:  "Invalid response from receiver",

	// EPG
	ErrEPGEmpty:      "No EPG data available",
	ErrEPGPartial:    "Partial EPG data available",
	ErrEPGTimeout:    "EPG request timeout",
	ErrEPGParseError: "Invalid EPG response",

	// Library
	ErrRootPathNotFound:     "Library path not found",
	ErrRootPermissionDenied: "Permission denied",
	ErrScanPartialFailure:   "Scan completed with errors",
	ErrScanTimeout:          "Scan incomplete (timeout)",

	// Playback
	ErrFFmpegNotFound: "Media processor unavailable",
	ErrLeaseExhausted: "All streams in use",
	ErrSessionCrashed: "Playback failed",
}

// SuggestedActions provides remediation guidance per error code.
var SuggestedActions = map[string][]string{
	ErrReceiverUnreachable: {
		"Check receiver network connectivity",
		"Verify receiver IP address in config",
		"Ensure receiver is powered on",
	},
	ErrUpstreamResultFalse: {
		"Check receiver recording path mount",
		"Verify OpenWebIF compatibility",
		"Review receiver logs for errors",
	},
	ErrRootPathNotFound: {
		"Verify mount point exists",
		"Check NAS/CIFS/NFS mount status",
		"Review library root configuration",
	},
	ErrRootPermissionDenied: {
		"Check filesystem permissions",
		"Verify xg2g user has read access",
		"Review mount options (e.g., noperm for CIFS)",
	},
}
