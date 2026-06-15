// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

type envLookupFunc func(string) (string, bool)

// ParseString reads a string from environment variable or returns default value.
// It logs the source (environment or default) for observability.
func ParseString(key, defaultValue string) string {
	return parseStringWithLookup(log.WithComponent("config"), currentProcessLookupEnv(), key, defaultValue)
}

// envPresent is the single source of truth for "is this env var effective?". It reports a
// value only when the key is set to a NON-EMPTY string; an unset OR explicitly-empty var is
// "not present" and the caller falls back to the lower-precedence value (file/default).
//
// This is deliberately the ONE definition of "set" shared by the env-merge
// (parseString/Int/Duration below) AND the conflict check (checkVODConflicts), so the two
// can never disagree on what counts as set. The empty-as-unset rule has a real reason: a
// set-but-empty sensitive key (a token/password var set to "") must not override and wipe a
// file-configured value, which would fail auth closed (lockout).
func envPresent(lookup envLookupFunc, key string) (string, bool) {
	if lookup == nil {
		lookup = currentProcessLookupEnv()
	}
	if value, ok := lookup(key); ok && value != "" {
		return value, true
	}
	return "", false
}

func parseStringWithLookup(logger zerolog.Logger, lookup envLookupFunc, key, defaultValue string) string {
	value, present := envPresent(lookup, key)
	if !present {
		logger.Debug().
			Str("key", key).
			Str("default", defaultValue).
			Str("source", "default").
			Msg("using default value (environment variable unset or empty)")
		return defaultValue
	}
	if lowerKey := strings.ToLower(key); strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "password") {
		// For sensitive vars, just log that it was set (never the value).
		logger.Debug().
			Str("key", key).
			Str("source", "environment").
			Bool("sensitive", true).
			Msg("using environment variable")
	} else {
		logger.Debug().
			Str("key", key).
			Str("value", value).
			Str("source", "environment").
			Msg("using environment variable")
	}
	return value
}

// ParseInt reads an integer from environment variable or returns default value.
// It validates the input and falls back to default on parse errors.
func ParseInt(key string, defaultValue int) int {
	return parseIntWithLookup(log.WithComponent("config"), currentProcessLookupEnv(), key, defaultValue)
}

// ParseDuration reads a duration from environment variable in Go duration format (e.g. "5s").
// It falls back to default on parse errors or empty variables and logs the choice.
func ParseDuration(key string, defaultValue time.Duration) time.Duration {
	return parseDurationWithLookup(log.WithComponent("config"), currentProcessLookupEnv(), key, defaultValue)
}

func parseIntWithLookup(logger zerolog.Logger, lookup envLookupFunc, key string, defaultValue int) int {
	v, present := envPresent(lookup, key)
	if !present {
		logger.Debug().
			Str("key", key).
			Int("default", defaultValue).
			Str("source", "default").
			Msg("using default value (environment variable unset or empty)")
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

func parseDurationWithLookup(logger zerolog.Logger, lookup envLookupFunc, key string, defaultValue time.Duration) time.Duration {
	v, present := envPresent(lookup, key)
	if !present {
		logger.Debug().
			Str("key", key).
			Dur("default", defaultValue).
			Str("source", "default").
			Msg("using default value (environment variable unset or empty)")
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

// ParseBool reads a boolean from environment variable or returns default value.
// It accepts "true", "false", "1", "0", "yes", "no" (case-insensitive).
func ParseBool(key string, defaultValue bool) bool {
	return parseBoolWithLookup(log.WithComponent("config"), currentProcessLookupEnv(), key, defaultValue)
}

// ReadOSRuntimeEnv reads all runtime environment variables from the current process
// environment and returns an immutable Env suitable for BuildSnapshot.
func ReadOSRuntimeEnv() (Env, error) {
	return ReadEnv(currentProcessGetEnv())
}

// ReadOSRuntimeEnvOrDefault reads the runtime Env from the current process environment.
// If reading fails, it returns DefaultEnv.
func ReadOSRuntimeEnvOrDefault() Env {
	env, err := ReadOSRuntimeEnv()
	if err != nil {
		return DefaultEnv()
	}
	return env
}

// ParseFloat reads a float64 from environment variable or returns default value.
func ParseFloat(key string, defaultValue float64) float64 {
	return parseFloatWithLookup(log.WithComponent("config"), currentProcessLookupEnv(), key, defaultValue)
}

func parseBoolWithLookup(logger zerolog.Logger, lookup envLookupFunc, key string, defaultValue bool) bool {
	if lookup == nil {
		lookup = currentProcessLookupEnv()
	}
	if v, ok := lookup(key); ok {
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

func parseFloatWithLookup(logger zerolog.Logger, lookup envLookupFunc, key string, defaultValue float64) float64 {
	if lookup == nil {
		lookup = currentProcessLookupEnv()
	}
	if v, ok := lookup(key); ok {
		if v == "" {
			logger.Debug().
				Str("key", key).
				Float64("default", defaultValue).
				Str("source", "default").
				Msg("using default value (environment variable is empty)")
			return defaultValue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			logger.Debug().
				Str("key", key).
				Float64("value", f).
				Str("source", "environment").
				Msg("using environment variable")
			return f
		}
		logger.Warn().
			Str("key", key).
			Str("value", v).
			Float64("default", defaultValue).
			Msg("invalid float in environment variable, using default")
		return defaultValue
	}
	logger.Debug().
		Str("key", key).
		Float64("default", defaultValue).
		Str("source", "default").
		Msg("using default value")
	return defaultValue
}

// expandEnv expands environment variables in the format ${VAR} or $VAR
func expandEnv(s string) string {
	return os.Expand(s, currentProcessGetEnv())
}
