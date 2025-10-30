//go:build !gpu
// +build !gpu

// Package transcoder provides stub implementations when GPU/FFI is disabled.
package transcoder

import "errors"

// GPUServer is a stub when built without GPU support.
type GPUServer struct{}

// NewGPUServer returns a stub GPU server instance.
func NewGPUServer(_ string, _ string) *GPUServer {
	return &GPUServer{}
}

// Start returns an error when built without GPU support.
func (s *GPUServer) Start() error {
	return errors.New("gpu server not available: build with -tags=gpu")
}

// Stop is a no-op stub.
func (s *GPUServer) Stop() error {
	return nil
}

// IsRunning always returns false (stub implementation).
func (s *GPUServer) IsRunning() bool {
	return false
}

// GetURL returns an empty string (stub implementation).
func (s *GPUServer) GetURL() string {
	return ""
}
