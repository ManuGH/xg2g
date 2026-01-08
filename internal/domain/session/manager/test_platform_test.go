package manager

import (
	"fmt"
	"os"
	"path/filepath"
)

// TestPlatform provides a Platform implementation for testing that performs real filesystem operations.
// Used in tests that assert cleanup behavior (like sweeper tests).
type TestPlatform struct {
	BaseDir string // Absolute temp directory for tests
}

func NewTestPlatform(baseDir string) *TestPlatform {
	return &TestPlatform{BaseDir: baseDir}
}

func (p *TestPlatform) Identity() (string, error) {
	return "test-platform-worker", nil
}

func (p *TestPlatform) RemoveAll(path string) error {
	// Safety: Verify path is absolute
	if !filepath.IsAbs(path) {
		return fmt.Errorf("refusing to remove non-absolute path: %s", path)
	}
	// Safety: Verify path is within test base directory to prevent accidental deletion
	if p.BaseDir != "" {
		rel, err := filepath.Rel(p.BaseDir, path)
		if err != nil || len(rel) > 1 && rel[:2] == ".." {
			return fmt.Errorf("refusing to remove path outside base dir: %s", path)
		}
	}
	return os.RemoveAll(path)
}

func (p *TestPlatform) Join(elem ...string) string {
	return filepath.Join(elem...)
}
