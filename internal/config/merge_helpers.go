// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

// Helper to parse recording path mappings: "/receiver/path=/local/path;/other=/mount"
//
//nolint:unused
func parseRecordingMappings(envVal string, defaults []RecordingPathMapping) []RecordingPathMapping {
	if envVal == "" {
		return defaults
	}
	var out []RecordingPathMapping
	entries := strings.Split(envVal, ";")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			continue
		}
		receiverRoot := strings.TrimSpace(kv[0])
		localRoot := strings.TrimSpace(kv[1])
		if receiverRoot == "" || localRoot == "" {
			continue
		}
		out = append(out, RecordingPathMapping{
			ReceiverRoot: receiverRoot,
			LocalRoot:    localRoot,
		})
	}
	if len(out) == 0 {
		return defaults
	}
	return out
}

// Helper to parse scoped tokens from XG2G_API_TOKENS.
// JSON array format is canonical; legacy "token=scopes;token2=scopes2" remains supported.
func parseScopedTokens(envVal string, defaults []ScopedToken) ([]ScopedToken, error) {
	trimmed := strings.TrimSpace(envVal)
	if trimmed == "" {
		return defaults, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		return parseScopedTokensJSON(trimmed)
	}
	if strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON must be an array of objects")
	}

	logger := log.WithComponent("config")
	logger.Warn().
		Str("key", "XG2G_API_TOKENS").
		Msg("legacy token format detected; JSON array is recommended")
	return parseScopedTokensLegacy(trimmed)
}

func parseScopedTokensJSON(raw string) ([]ScopedToken, error) {
	var entries []scopedTokenJSON
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON parse failed: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("XG2G_API_TOKENS JSON array is empty")
	}
	seen := make(map[string]struct{}, len(entries))
	out := make([]ScopedToken, 0, len(entries))
	for _, entry := range entries {
		token := strings.TrimSpace(entry.Token)
		if token == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS token is empty")
		}
		if _, ok := seen[token]; ok {
			return nil, fmt.Errorf("XG2G_API_TOKENS duplicate token %q", token)
		}
		seen[token] = struct{}{}

		scopes := make([]string, 0, len(entry.Scopes))
		for _, scope := range entry.Scopes {
			scope = strings.TrimSpace(scope)
			if scope == "" {
				return nil, fmt.Errorf("XG2G_API_TOKENS scopes must not be empty for token %q", token)
			}
			scopes = append(scopes, scope)
		}
		if len(scopes) == 0 {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		out = append(out, ScopedToken{
			Token:  token,
			Scopes: scopes,
		})
	}
	return out, nil
}

func parseScopedTokensLegacy(raw string) ([]ScopedToken, error) {
	entries := strings.Split(raw, ";")
	var out []ScopedToken
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("XG2G_API_TOKENS legacy entry must be token=scopes: %q", entry)
		}
		token := strings.TrimSpace(kv[0])
		scopesRaw := strings.TrimSpace(kv[1])
		if token == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS token is empty")
		}
		if _, ok := seen[token]; ok {
			return nil, fmt.Errorf("XG2G_API_TOKENS duplicate token %q", token)
		}
		seen[token] = struct{}{}
		if scopesRaw == "" {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		scopes := parseCommaSeparated(scopesRaw, nil)
		if len(scopes) == 0 {
			return nil, fmt.Errorf("XG2G_API_TOKENS scopes must be set for token %q", token)
		}
		out = append(out, ScopedToken{
			Token:  token,
			Scopes: scopes,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("XG2G_API_TOKENS has no valid token entries")
	}
	return out, nil
}

// Helper to parse comma-separated list
func parseCommaSeparated(envVal string, defaults []string) []string {
	if envVal == "" {
		return defaults
	}
	var out []string
	parts := strings.Split(envVal, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseCommaSeparatedInts(envVal string, defaults []int) []int {
	if envVal == "" {
		return defaults
	}
	logger := log.WithComponent("config")
	var out []int
	parts := strings.Split(envVal, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		val, err := strconv.Atoi(p)
		if err != nil {
			logger.Warn().
				Str("value", p).
				Msg("invalid integer in environment list; skipping")
			continue
		}
		out = append(out, val)
	}
	return out
}
