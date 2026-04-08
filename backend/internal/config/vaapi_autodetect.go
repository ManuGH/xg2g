package config

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
)

const vaapiDeviceEnvKey = "XG2G_VAAPI_DEVICE"

// autoDetectVAAPIDevice keeps explicit file/env configuration authoritative,
// while allowing containerized deployments to auto-discover a mounted
// /dev/dri/renderD* node when no override was provided.
func autoDetectVAAPIDevice(lookup envLookupFunc, configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}

	if lookup == nil {
		lookup = currentProcessLookupEnv()
	}
	if _, exists := lookup(vaapiDeviceEnvKey); exists {
		return ""
	}

	entries, err := currentProcessReadDir()("/dev/dri")
	if err != nil {
		return ""
	}

	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" || !strings.HasPrefix(name, "renderD") {
			continue
		}
		candidates = append(candidates, filepath.Join("/dev/dri", name))
	}
	if len(candidates) == 0 {
		return ""
	}

	slices.Sort(candidates)
	detected := candidates[0]
	logger := log.WithComponent("config")
	logger.Info().
		Str("device", detected).
		Msg("auto-detected VAAPI device from /dev/dri")
	return detected
}
