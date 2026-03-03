// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package http

// FileMetrics provides telemetry for file serving operations.
// This interface allows control layer to remain decoupled from
// specific metrics implementations while preserving observability.
type FileMetrics interface {
	// Denied records a file request denial with the given reason.
	// Common reasons: "method_not_allowed", "forbidden_extension",
	// "path_escape", "directory_listing", "not_found", "internal_error"
	Denied(reason string)

	// Allowed records a successful file request.
	Allowed()

	// CacheHit records a 304 Not Modified response (ETag match).
	CacheHit()

	// CacheMiss records a 200 OK response (content served).
	CacheMiss()
}

// noopFileMetrics provides a no-op implementation for when metrics are disabled.
type noopFileMetrics struct{}

func (noopFileMetrics) Denied(reason string) {}
func (noopFileMetrics) Allowed()             {}
func (noopFileMetrics) CacheHit()            {}
func (noopFileMetrics) CacheMiss()           {}

// NewNoopFileMetrics returns a no-op FileMetrics implementation.
func NewNoopFileMetrics() FileMetrics {
	return noopFileMetrics{}
}
