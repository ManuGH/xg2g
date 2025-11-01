// SPDX-License-Identifier: MIT

package jobs

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/validate"
)

// validateConfig validates the configuration for refresh operations
func validateConfig(cfg Config) error {
	// Use centralized validation package
	v := validate.New()

	v.URL("OWIBase", cfg.OWIBase, []string{"http", "https"})
	v.Port("StreamPort", cfg.StreamPort)
	v.Directory("DataDir", cfg.DataDir, false)

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}

// validateOptions validates the options for refresh operations
func validateOptions(opts Options) error {
	if opts.Parallelism < 0 {
		return fmt.Errorf("parallelism must be >= 0, got %d", opts.Parallelism)
	}
	return nil
}

// sanitizeFilename sanitizes a playlist filename to prevent path traversal attacks
func sanitizeFilename(name string) (string, error) {
	if name == "" {
		return "playlist.m3u", nil
	}

	// Strip any directory components
	base := filepath.Base(name)

	// Reject if still contains traversal
	if strings.Contains(base, "..") {
		return "", fmt.Errorf("invalid filename: contains traversal")
	}

	// Clean the filename
	cleaned := filepath.Clean(base)

	// Ensure it's local
	if !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("invalid filename: not local")
	}

	// Validate extension
	ext := filepath.Ext(cleaned)
	if ext != ".m3u" && ext != ".m3u8" {
		cleaned += ".m3u"
	}

	return cleaned, nil
}

// clampConcurrency ensures concurrency is within sane bounds [1, max]
func clampConcurrency(value, defaultValue, max int) int {
	if value < 1 {
		if defaultValue < 1 {
			return 1
		}
		return defaultValue
	}
	if value > max {
		return max
	}
	return value
}
