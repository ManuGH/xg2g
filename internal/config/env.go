// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

// ParseString reads a string from environment variable or returns default value.
// It logs the source (environment or default) for observability.
func ParseString(key, defaultValue string) string {
	return parseStringWithLogger(log.WithComponent("config"), key, defaultValue)
}

// parseStringWithLogger reads an environment variable with custom logger.
func parseStringWithLogger(logger zerolog.Logger, key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		lowerKey := strings.ToLower(key)
		switch {
		case strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "password"):
			// For sensitive vars, just log that it was set
			logger.Debug().
				Str("key", key).
				Str("source", "environment").
				Bool("sensitive", true).
				Msg("using environment variable")
		case value == "":
			logger.Debug().
				Str("key", key).
				Str("default", defaultValue).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return defaultValue
		default:
			logger.Debug().
				Str("key", key).
				Str("value", value).
				Str("source", "environment").
				Msg("using environment variable")
		}
		return value
	}
	logger.Debug().
		Str("key", key).
		Str("default", defaultValue).
		Str("source", "default").
		Msg("using default value")
	return defaultValue
}

// ParseInt reads an integer from environment variable or returns default value.
// It validates the input and falls back to default on parse errors.
func ParseInt(key string, defaultValue int) int {
	logger := log.WithComponent("config")
	if v, ok := os.LookupEnv(key); ok {
		if v == "" {
			logger.Debug().
				Str("key", key).
				Int("default", defaultValue).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return defaultValue
		}
		if i, err := strconv.Atoi(v); err == nil {
			logger.Debug().
				Str("key", key).
				Int("value", i).
				Str("source", "environment").
				Msg("using environment variable")
			return i
		}
		logger.Warn().
			Str("key", key).
			Str("value", v).
			Int("default", defaultValue).
			Msg("invalid integer in environment variable, using default")
		return defaultValue
	}
	logger.Debug().
		Str("key", key).
		Int("default", defaultValue).
		Str("source", "default").
		Msg("using default value")
	return defaultValue
}

// ParseDuration reads a duration from environment variable in Go duration format (e.g. "5s").
// It falls back to default on parse errors or empty variables and logs the choice.
func ParseDuration(key string, defaultValue time.Duration) time.Duration {
	logger := log.WithComponent("config")
	if v, ok := os.LookupEnv(key); ok {
		if v == "" {
			logger.Debug().
				Str("key", key).
				Dur("default", defaultValue).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return defaultValue
		}
		if d, err := time.ParseDuration(v); err == nil {
			logger.Debug().
				Str("key", key).
				Dur("value", d).
				Str("source", "environment").
				Msg("using environment variable")
			return d
		}
		logger.Warn().
			Str("key", key).
			Str("value", v).
			Dur("default", defaultValue).
			Msg("invalid duration in environment variable, using default")
		return defaultValue
	}
	logger.Debug().
		Str("key", key).
		Dur("default", defaultValue).
		Str("source", "default").
		Msg("using default value")
	return defaultValue
}

// ParseBool reads a boolean from environment variable or returns default value.
// It accepts "true", "false", "1", "0", "yes", "no" (case-insensitive).
func ParseBool(key string, defaultValue bool) bool {
	logger := log.WithComponent("config")
	if v, ok := os.LookupEnv(key); ok {
		if v == "" {
			logger.Debug().
				Str("key", key).
				Bool("default", defaultValue).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return defaultValue
		}
		lowerV := strings.ToLower(v)
		switch lowerV {
		case "true", "1", "yes":
			logger.Debug().
				Str("key", key).
				Bool("value", true).
				Str("source", "environment").
				Msg("using environment variable")
			return true
		case "false", "0", "no":
			logger.Debug().
				Str("key", key).
				Bool("value", false).
				Str("source", "environment").
				Msg("using environment variable")
			return false
		default:
			logger.Warn().
				Str("key", key).
				Str("value", v).
				Bool("default", defaultValue).
				Msg("invalid boolean in environment variable, using default")
			return defaultValue
		}
	}
	logger.Debug().
		Str("key", key).
		Bool("default", defaultValue).
		Str("source", "default").
		Msg("using default value")
	return defaultValue
}
