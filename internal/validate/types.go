// SPDX-License-Identifier: MIT
package validate

// LogLevel represents valid log levels
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
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
