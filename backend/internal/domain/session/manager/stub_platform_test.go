package manager

import (
	"path/filepath"
)

// StubPlatform provides a simple Platform implementation for testing.
type StubPlatform struct{}

func NewStubPlatform() *StubPlatform {
	return &StubPlatform{}
}

func (p *StubPlatform) Identity() (string, error) {
	return "test-host-1234-uuid", nil
}

func (p *StubPlatform) RemoveAll(path string) error {
	// Stub: no-op in tests
	return nil
}

func (p *StubPlatform) Join(elem ...string) string {
	return filepath.Join(elem...)
}
