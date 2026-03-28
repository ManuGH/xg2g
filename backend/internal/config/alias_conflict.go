// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type aliasPresence struct {
	hasOpenWebIF bool
	openWebIF    map[string]bool
	enigma2      map[string]bool
}

const enigma2MigrationGuide = "docs/guides/CONFIGURATION.md#enigma2"

var openWebIFToEnigma2Key = map[string]string{
	"backoff":         "enigma2.backoff",
	"baseUrl":         "enigma2.baseUrl",
	"maxBackoff":      "enigma2.maxBackoff",
	"password":        "enigma2.password",
	"retries":         "enigma2.retries",
	"streamPort":      "enigma2.streamPort",
	"timeout":         "enigma2.timeout",
	"useWebIFStreams": "enigma2.useWebIFStreams",
	"username":        "enigma2.username",
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
	if openRaw, ok := raw["openWebIF"]; ok {
		presence.hasOpenWebIF = true
		if open, ok := openRaw.(map[string]any); ok {
			for key := range open {
				presence.openWebIF[key] = true
			}
		}
	}
	if e2, ok := raw["enigma2"].(map[string]any); ok {
		for key := range e2 {
			presence.enigma2[key] = true
		}
	}
	return presence, nil
}

func rejectLegacyOpenWebIFYAML(presence *aliasPresence) error {
	if presence == nil || !presence.hasOpenWebIF {
		return nil
	}
	return legacyOpenWebIFYAMLError(legacyOpenWebIFKeysFromPresence(presence))
}

func legacyOpenWebIFYAMLError(keys []string) error {
	if len(keys) == 0 {
		return fmt.Errorf("legacy YAML section openWebIF is no longer supported. Migrate to enigma2.*. See %s", enigma2MigrationGuide)
	}

	migrations := make([]string, 0, len(keys))
	for _, key := range keys {
		target, ok := openWebIFToEnigma2Key[key]
		if !ok {
			target = "enigma2.*"
		}
		migrations = append(migrations, fmt.Sprintf("openWebIF.%s -> %s", key, target))
	}

	return fmt.Errorf("legacy YAML key(s) detected: %s. openWebIF.* is no longer supported; migrate to enigma2.*. See %s",
		strings.Join(migrations, ", "), enigma2MigrationGuide)
}

func legacyOpenWebIFKeysFromPresence(presence *aliasPresence) []string {
	if presence == nil || !presence.hasOpenWebIF {
		return nil
	}
	keys := make([]string, 0, len(presence.openWebIF))
	for key := range presence.openWebIF {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func legacyOpenWebIFKeysFromConfig(src *FileConfig) []string {
	if src == nil {
		return nil
	}

	keys := make([]string, 0, len(openWebIFToEnigma2Key))
	if strings.TrimSpace(src.OpenWebIF.BaseURL) != "" {
		keys = append(keys, "baseUrl")
	}
	if strings.TrimSpace(src.OpenWebIF.Username) != "" {
		keys = append(keys, "username")
	}
	if strings.TrimSpace(src.OpenWebIF.Password) != "" {
		keys = append(keys, "password")
	}
	if strings.TrimSpace(src.OpenWebIF.Timeout) != "" {
		keys = append(keys, "timeout")
	}
	if src.OpenWebIF.Retries != 0 {
		keys = append(keys, "retries")
	}
	if strings.TrimSpace(src.OpenWebIF.Backoff) != "" {
		keys = append(keys, "backoff")
	}
	if strings.TrimSpace(src.OpenWebIF.MaxBackoff) != "" {
		keys = append(keys, "maxBackoff")
	}
	if src.OpenWebIF.StreamPort != 0 {
		keys = append(keys, "streamPort")
	}
	if src.OpenWebIF.UseWebIF != nil {
		keys = append(keys, "useWebIFStreams")
	}
	sort.Strings(keys)
	return keys
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
		{
			openKey: "streamPort",
			e2Key:   "streamPort",
			equal: func(cfg *FileConfig) bool {
				if cfg.Enigma2.StreamPort == nil {
					return false
				}
				return cfg.OpenWebIF.StreamPort == *cfg.Enigma2.StreamPort
			},
		},
		{
			openKey: "useWebIFStreams",
			e2Key:   "useWebIFStreams",
			equal: func(cfg *FileConfig) bool {
				if cfg.OpenWebIF.UseWebIF == nil || cfg.Enigma2.UseWebIF == nil {
					return false
				}
				return *cfg.OpenWebIF.UseWebIF == *cfg.Enigma2.UseWebIF
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

func aliasConflictError(openKey, e2Key string) error {
	return fmt.Errorf("openWebIF.%s conflicts with enigma2.%s (compat alias). Prefer enigma2.* and remove openWebIF.*", openKey, e2Key)
}
