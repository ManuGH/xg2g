// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import "errors"

var (
	// ErrMissingLogger is returned when logger is not provided
	ErrMissingLogger = errors.New("logger is required")

	// ErrMissingAPIHandler is returned when API handler is not provided
	ErrMissingAPIHandler = errors.New("API handler is required")

	// ErrMissingManager is returned when a daemon app is created without a manager.
	ErrMissingManager = errors.New("manager is required")

	// ErrManagerNotStarted is returned when trying to shutdown a manager that hasn't started
	ErrManagerNotStarted = errors.New("manager not started")

	// ErrServerStartFailed is returned when a server fails to start
	ErrServerStartFailed = errors.New("server failed to start")

	// ErrMissingMediaPipeline is returned when v3 is enabled without a pipeline.
	ErrMissingMediaPipeline = errors.New("media pipeline is required when engine is enabled")
)
