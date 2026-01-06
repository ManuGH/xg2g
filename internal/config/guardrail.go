package config

import (
	"os"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

// CheckLegacyEnv scans environment variables for legacy keys and exits if any are found.
// This enforces the "Canonical Only" policy for pre-release.
func CheckLegacyEnv() {
	legacyPrefix := "XG2G_V3_"
	legacyKeys := []string{
		"XG2G_V3_WORKER_ENABLED",
		"XG2G_V3_WORKER_MODE",
		"XG2G_V3_IDLE_TIMEOUT",
		"XG2G_V3_STORE_BACKEND",
		"XG2G_V3_STORE_PATH",
		"XG2G_V3_HLS_ROOT",
		"XG2G_V3_TUNER_SLOTS",
		"XG2G_V3_CONFIG_STRICT",
		"XG2G_V3_E2_HOST", // And so on, prefix catch-all covers most
		"XG2G_FFMPEG_PATH",
	}

	logger := log.WithComponent("config")

	found := false
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		key := pair[0]

		if strings.HasPrefix(key, legacyPrefix) {
			logger.Error().Str("key", key).Msg("Legacy configuration key detected. Pre-release versions require canonical keys (e.g. XG2G_ENGINE_...).")
			found = true
		} else {
			for _, lk := range legacyKeys {
				if key == lk {
					logger.Error().Str("key", key).Msg("Legacy configuration key detected.")
					found = true
				}
			}
		}
	}

	if found {
		logger.Fatal().Msg("Startup aborted due to legacy configuration keys. Please migrate to canonical Env Vars (see CONFIGURATION.md).")
	}
}
