package problem

// Canonical Header Names
const (
	// HeaderRequestID is the canonical header for request correlation.
	HeaderRequestID = "X-Request-ID"
)

// Canonical JSON Field Names
const (
	// JSONKeyRequestID is the canonical JSON key for request correlation in DTOs.
	JSONKeyRequestID = "requestId"

	// FallbackRequestID is the explicit stop-the-line sentinel when request
	// correlation middleware failed to populate a real request ID.
	FallbackRequestID = "FALLBACK-TRUTH-MISSING"
)
