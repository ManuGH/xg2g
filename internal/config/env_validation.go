// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

var securitySensitiveEnvTokens = []string{
	"AUTH",
	"TOKEN",
	"PASS",
	"PASSWORD",
	"TLS",
	"HTTPS",
	"TRUST",
	"PROXY",
	"ORIGIN",
	"CORS",
}

// ValidateEnvUsage detects unknown XG2G_* keys (dead flags / typos).
// In strict mode, unknown security-sensitive keys fail fast.
func (l *Loader) ValidateEnvUsage(strict bool) error {
	registry, err := GetRegistry()
	if err != nil {
		return fmt.Errorf("get config registry: %w", err)
	}

	known := make(map[string]struct{}, len(registry.ByEnv))
	for key := range registry.ByEnv {
		if strings.TrimSpace(key) != "" {
			known[key] = struct{}{}
		}
	}
	for _, key := range KnownRuntimeEnvKeys() {
		known[key] = struct{}{}
	}
	for _, key := range removedEnvKeys {
		known[key.Key] = struct{}{}
	}

	if deps, depErr := LoadDeprecations(); depErr == nil {
		for _, d := range deps {
			if strings.HasPrefix(strings.TrimSpace(d.Key), "XG2G_") {
				known[d.Key] = struct{}{}
			}
		}
	}

	unknown := make([]string, 0)
	fatal := make([]string, 0)

	for _, pair := range l.envEnviron() {
		key := strings.SplitN(pair, "=", 2)[0]
		if !strings.HasPrefix(key, "XG2G_") {
			continue
		}

		if strings.HasPrefix(key, "XG2G_V3_") {
			continue // handled by CheckLegacyEnvWithEnviron
		}
		if key == "XG2G_FFMPEG_PATH" {
			continue // handled by CheckLegacyEnvWithEnviron (split literal in guardrail)
		}

		if _, ok := known[key]; ok {
			continue
		}
		if _, consumed := l.ConsumedEnvKeys[key]; consumed {
			continue
		}

		unknown = append(unknown, key)
		if strict && isSecuritySensitiveEnvKey(key) {
			fatal = append(fatal, key)
		}
	}

	if len(unknown) > 0 {
		sort.Strings(unknown)
		logger := log.WithComponent("config")
		for _, key := range unknown {
			logger.Warn().
				Str("key", key).
				Msg("unknown XG2G env key detected (dead flag or typo)")
		}
	}

	if len(fatal) > 0 {
		sort.Strings(fatal)
		return fmt.Errorf("unknown security-sensitive XG2G env keys: %s", strings.Join(fatal, ", "))
	}

	return nil
}

func isSecuritySensitiveEnvKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	for _, token := range securitySensitiveEnvTokens {
		if strings.Contains(upper, token) {
			return true
		}
	}
	return false
}
