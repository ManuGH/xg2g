// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type aliasPresence struct {
	openWebIF map[string]bool
	enigma2   map[string]bool
}

func parseAliasPresence(data []byte) (*aliasPresence, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	presence := &aliasPresence{
		openWebIF: map[string]bool{},
		enigma2:   map[string]bool{},
	}
	if open, ok := raw["openWebIF"].(map[string]any); ok {
		for key := range open {
			presence.openWebIF[key] = true
		}
	}
	if e2, ok := raw["enigma2"].(map[string]any); ok {
		for key := range e2 {
			presence.enigma2[key] = true
		}
	}
	return presence, nil
}

func (l *Loader) checkAliasConflicts(src *FileConfig) error {
	if l.filePresence == nil {
		return nil
	}
	presence := l.filePresence

	type aliasPair struct {
		openKey string
		e2Key   string
		equal   func(*FileConfig) bool
	}

	pairs := []aliasPair{
		{
			openKey: "baseUrl",
			e2Key:   "baseUrl",
			equal: func(cfg *FileConfig) bool {
				return equalString(cfg.OpenWebIF.BaseURL, cfg.Enigma2.BaseURL)
			},
		},
		{
			openKey: "username",
			e2Key:   "username",
			equal: func(cfg *FileConfig) bool {
				return equalString(cfg.OpenWebIF.Username, cfg.Enigma2.Username)
			},
		},
		{
			openKey: "password",
			e2Key:   "password",
			equal: func(cfg *FileConfig) bool {
				return equalString(cfg.OpenWebIF.Password, cfg.Enigma2.Password)
			},
		},
		{
			openKey: "timeout",
			e2Key:   "timeout",
			equal: func(cfg *FileConfig) bool {
				return equalDurationString(cfg.OpenWebIF.Timeout, cfg.Enigma2.Timeout)
			},
		},
		{
			openKey: "retries",
			e2Key:   "retries",
			equal: func(cfg *FileConfig) bool {
				return cfg.OpenWebIF.Retries == cfg.Enigma2.Retries
			},
		},
		{
			openKey: "backoff",
			e2Key:   "backoff",
			equal: func(cfg *FileConfig) bool {
				return equalDurationString(cfg.OpenWebIF.Backoff, cfg.Enigma2.Backoff)
			},
		},
		{
			openKey: "maxBackoff",
			e2Key:   "maxBackoff",
			equal: func(cfg *FileConfig) bool {
				return equalDurationString(cfg.OpenWebIF.MaxBackoff, cfg.Enigma2.MaxBackoff)
			},
		},
	}

	for _, pair := range pairs {
		if !presence.openWebIF[pair.openKey] || !presence.enigma2[pair.e2Key] {
			continue
		}
		if pair.equal(src) {
			continue
		}
		return aliasConflictError(pair.openKey, pair.e2Key)
	}

	return nil
}

func equalString(a, b string) bool {
	return strings.TrimSpace(expandEnv(a)) == strings.TrimSpace(expandEnv(b))
}

func equalDurationString(a, b string) bool {
	a = strings.TrimSpace(expandEnv(a))
	b = strings.TrimSpace(expandEnv(b))
	if a == "" && b == "" {
		return true
	}
	da, errA := time.ParseDuration(a)
	db, errB := time.ParseDuration(b)
	if errA == nil && errB == nil {
		return da == db
	}
	return a == b
}

func (l *Loader) checkAliasEnvConflicts(src *FileConfig) error {
	if l.filePresence == nil {
		return nil
	}
	presence := l.filePresence

	// YAML openWebIF vs ENV enigma2
	if presence.openWebIF["baseUrl"] {
		if v, ok := envString("XG2G_E2_HOST"); ok && !equalString(src.OpenWebIF.BaseURL, v) {
			return aliasConflictError("baseUrl", "baseUrl")
		}
	}
	if presence.openWebIF["username"] {
		if v, ok := envString("XG2G_E2_USER"); ok && !equalString(src.OpenWebIF.Username, v) {
			return aliasConflictError("username", "username")
		}
	}
	if presence.openWebIF["password"] {
		if v, ok := envString("XG2G_E2_PASS"); ok && !equalString(src.OpenWebIF.Password, v) {
			return aliasConflictError("password", "password")
		}
	}
	if presence.openWebIF["timeout"] {
		if d, ok := envDuration("XG2G_E2_TIMEOUT"); ok && !equalDurationEnv(src.OpenWebIF.Timeout, d) {
			return aliasConflictError("timeout", "timeout")
		}
	}
	if presence.openWebIF["retries"] {
		if v, ok := envInt("XG2G_E2_RETRIES"); ok && src.OpenWebIF.Retries != v {
			return aliasConflictError("retries", "retries")
		}
	}
	if presence.openWebIF["backoff"] {
		if d, ok := envDuration("XG2G_E2_BACKOFF"); ok && !equalDurationEnv(src.OpenWebIF.Backoff, d) {
			return aliasConflictError("backoff", "backoff")
		}
	}
	if presence.openWebIF["maxBackoff"] {
		if d, ok := envDuration("XG2G_E2_MAX_BACKOFF"); ok && !equalDurationEnv(src.OpenWebIF.MaxBackoff, d) {
			return aliasConflictError("maxBackoff", "maxBackoff")
		}
	}

	// YAML enigma2 vs ENV openWebIF
	if presence.enigma2["baseUrl"] {
		if v, ok := envString("XG2G_OWI_BASE"); ok && !equalString(src.Enigma2.BaseURL, v) {
			return aliasConflictError("baseUrl", "baseUrl")
		}
	}
	if presence.enigma2["username"] {
		if v, ok := envString("XG2G_OWI_USER"); ok && !equalString(src.Enigma2.Username, v) {
			return aliasConflictError("username", "username")
		}
	}
	if presence.enigma2["password"] {
		if v, ok := envString("XG2G_OWI_PASS"); ok && !equalString(src.Enigma2.Password, v) {
			return aliasConflictError("password", "password")
		}
	}
	if presence.enigma2["timeout"] {
		if d, ok := envDurationMS("XG2G_OWI_TIMEOUT_MS"); ok && !equalDurationEnv(src.Enigma2.Timeout, d) {
			return aliasConflictError("timeout", "timeout")
		}
	}
	if presence.enigma2["retries"] {
		if v, ok := envInt("XG2G_OWI_RETRIES"); ok && src.Enigma2.Retries != v {
			return aliasConflictError("retries", "retries")
		}
	}
	if presence.enigma2["backoff"] {
		if d, ok := envDurationMS("XG2G_OWI_BACKOFF_MS"); ok && !equalDurationEnv(src.Enigma2.Backoff, d) {
			return aliasConflictError("backoff", "backoff")
		}
	}
	if presence.enigma2["maxBackoff"] {
		if d, ok := envDurationMS("XG2G_OWI_MAX_BACKOFF_MS"); ok && !equalDurationEnv(src.Enigma2.MaxBackoff, d) {
			return aliasConflictError("maxBackoff", "maxBackoff")
		}
	}

	return nil
}

func (l *Loader) checkAliasEnvToEnvConflicts() error {
	if owi, ok := envString("XG2G_OWI_BASE"); ok {
		if e2, ok := envString("XG2G_E2_HOST"); ok && !equalString(owi, e2) {
			return aliasConflictError("baseUrl", "baseUrl")
		}
	}
	if owi, ok := envString("XG2G_OWI_USER"); ok {
		if e2, ok := envString("XG2G_E2_USER"); ok && !equalString(owi, e2) {
			return aliasConflictError("username", "username")
		}
	}
	if owi, ok := envString("XG2G_OWI_PASS"); ok {
		if e2, ok := envString("XG2G_E2_PASS"); ok && !equalString(owi, e2) {
			return aliasConflictError("password", "password")
		}
	}
	if owi, ok := envDurationMS("XG2G_OWI_TIMEOUT_MS"); ok {
		if e2, ok := envDuration("XG2G_E2_TIMEOUT"); ok && owi != e2 {
			return aliasConflictError("timeout", "timeout")
		}
	}
	if owi, ok := envInt("XG2G_OWI_RETRIES"); ok {
		if e2, ok := envInt("XG2G_E2_RETRIES"); ok && owi != e2 {
			return aliasConflictError("retries", "retries")
		}
	}
	if owi, ok := envDurationMS("XG2G_OWI_BACKOFF_MS"); ok {
		if e2, ok := envDuration("XG2G_E2_BACKOFF"); ok && owi != e2 {
			return aliasConflictError("backoff", "backoff")
		}
	}
	if owi, ok := envDurationMS("XG2G_OWI_MAX_BACKOFF_MS"); ok {
		if e2, ok := envDuration("XG2G_E2_MAX_BACKOFF"); ok && owi != e2 {
			return aliasConflictError("maxBackoff", "maxBackoff")
		}
	}

	return nil
}

func aliasConflictError(openKey, e2Key string) error {
	return fmt.Errorf("openWebIF.%s conflicts with enigma2.%s (compat alias). Prefer enigma2.* and remove openWebIF.*.", openKey, e2Key)
}

func equalDurationEnv(yamlValue string, envValue time.Duration) bool {
	d, ok := parseDurationString(yamlValue)
	if !ok {
		return true
	}
	return d == envValue
}

func parseDurationString(value string) (time.Duration, bool) {
	value = strings.TrimSpace(expandEnv(value))
	if value == "" {
		return 0, true
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, false
	}
	return d, true
}

func envString(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

func envInt(key string) (int, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	out, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return out, true
}

func envDuration(key string) (time.Duration, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	out, err := time.ParseDuration(v)
	if err != nil {
		return 0, false
	}
	return out, true
}

func envDurationMS(key string) (time.Duration, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return time.Duration(parsed) * time.Millisecond, true
}
