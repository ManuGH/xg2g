package recordings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// RecordingCacheDir returns the canonical directory for a recording's assets.
// It follows the layout: <hlsRoot>/recordings/<sha256(serviceRef)>
func RecordingCacheDir(hlsRoot, serviceRef string) (string, error) {
	if strings.TrimSpace(hlsRoot) == "" {
		return "", fmt.Errorf("hls root not configured")
	}
	return filepath.Join(hlsRoot, "recordings", RecordingCacheKey(serviceRef)), nil
}

// RecordingCacheKey returns the stable hash key for a serviceRef.
func RecordingCacheKey(serviceRef string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(serviceRef)))
	return hex.EncodeToString(sum[:])
}
