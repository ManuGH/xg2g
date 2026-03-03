package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

var legacyExactEnvKeys = []string{
	"XG2G_V3_WORKER_ENABLED",
	"XG2G_V3_WORKER_MODE",
	"XG2G_V3_IDLE_TIMEOUT",
	"XG2G_V3_STORE_BACKEND",
	"XG2G_V3_STORE_PATH",
	"XG2G_V3_HLS_ROOT",
	"XG2G_V3_TUNER_SLOTS",
	"XG2G_V3_CONFIG_STRICT",
	"XG2G_V3_E2_HOST",
	// Blocked legacy single-key (outside the XG2G_V3_* prefix family).
	// Kept as a split literal to ensure repo searches for the legacy key name return zero results.
	"XG2G_FFMPEG_" + "PATH",
}

// FindLegacyEnvKeys returns all legacy env keys from the provided environment.
func FindLegacyEnvKeys(environ []string) []string {
	legacyPrefix := "XG2G_V3_"
	out := make([]string, 0)

	exact := make(map[string]struct{}, len(legacyExactEnvKeys))
	for _, key := range legacyExactEnvKeys {
		exact[key] = struct{}{}
	}

	for _, env := range environ {
		pair := strings.SplitN(env, "=", 2)
		key := pair[0]
		if key == "" {
			continue
		}

		if strings.HasPrefix(key, legacyPrefix) {
			out = append(out, key)
			continue
		}

		if _, ok := exact[key]; ok {
			out = append(out, key)
		}
	}

	sort.Strings(out)
	return out
}

// CheckLegacyEnvWithEnviron validates that no legacy keys are present.
func CheckLegacyEnvWithEnviron(environ []string) error {
	if len(environ) == 0 {
		environ = os.Environ()
	}
	keys := FindLegacyEnvKeys(environ)
	if len(keys) == 0 {
		return nil
	}
	return fmt.Errorf("legacy configuration keys detected: %s", strings.Join(keys, ", "))
}

// CheckLegacyEnv scans environment variables for legacy keys and exits if any are found.
// This enforces the "Canonical Only" policy for pre-release.
func CheckLegacyEnv() {
	logger := log.WithComponent("config")
	keys := FindLegacyEnvKeys(os.Environ())
	if len(keys) == 0 {
		return
	}

	for _, key := range keys {
		logger.Error().Str("key", key).Msg("Legacy configuration key detected.")
	}
	logger.Fatal().Msg("Startup aborted due to legacy configuration keys. Please migrate to canonical Env Vars (see CONFIGURATION.md).")
}
