package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// OSPlatform implements ports.Platform using standard OS operations.
type OSPlatform struct{}

func NewOSPlatform() *OSPlatform {
	return &OSPlatform{}
}

func (p *OSPlatform) Identity() (string, error) {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String()), nil
}

func (p *OSPlatform) RemoveAll(path string) error {
	// Safety: Verify path is absolute to prevent accidental deletion
	if !filepath.IsAbs(path) {
		return fmt.Errorf("refusing to remove non-absolute path: %s", path)
	}
	return os.RemoveAll(path)
}

func (p *OSPlatform) Join(elem ...string) string {
	return filepath.Join(elem...)
}
