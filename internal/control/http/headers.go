package http

// Canonical Header Names
const (
	// HeaderRequestID is the canonical header for request correlation.
	// Team-Red: Must be consistent across Middleware, ProblemWriter, and Tests.
	HeaderRequestID = "X-Request-ID"
)

// Canonical JSON Field Names
const (
	// JSONKeyRequestID is the canonical JSON key for request correlation in DTOs.
	JSONKeyRequestID = "requestId"
)
