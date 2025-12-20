// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

// Package validate provides configuration validation utilities for the xg2g application.
package validate

// LogLevel represents valid log levels
type LogLevel string

// Log level constants define the available logging severity levels.
const (
	// LogLevelDebug enables debug-level logging
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo enables info-level logging
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn enables warn-level logging
	LogLevelWarn LogLevel = "warn"
	// LogLevelError enables error-level logging
	LogLevelError LogLevel = "error"
)

// IsValid checks if the log level is valid
func (l LogLevel) IsValid() bool {
	switch l {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return true
	default:
		return false
	}
}

// String returns the string representation
func (l LogLevel) String() string {
	return string(l)
}

// ParseLogLevel parses a string into a LogLevel
func ParseLogLevel(s string) (LogLevel, error) {
	level := LogLevel(s)
	if !level.IsValid() {
		return "", ErrInvalidLogLevel
	}
	return level, nil
}

// Common validation errors
var (
	ErrInvalidLogLevel = &Error{
		Field:   "logLevel",
		Message: "invalid log level (must be: debug, info, warn, error)",
	}
)
