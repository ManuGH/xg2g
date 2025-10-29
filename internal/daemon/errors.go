// SPDX-License-Identifier: MIT

package daemon

import "errors"

var (
	// ErrMissingLogger is returned when logger is not provided
	ErrMissingLogger = errors.New("logger is required")

	// ErrMissingAPIHandler is returned when API handler is not provided
	ErrMissingAPIHandler = errors.New("API handler is required")

	// ErrManagerNotStarted is returned when trying to shutdown a manager that hasn't started
	ErrManagerNotStarted = errors.New("manager not started")

	// ErrServerStartFailed is returned when a server fails to start
	ErrServerStartFailed = errors.New("server failed to start")
)
