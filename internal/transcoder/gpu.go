//go:build gpu
// +build gpu

// SPDX-License-Identifier: MIT

package transcoder

/*
#cgo LDFLAGS: -L${SRCDIR}/../../transcoder/target/release -lxg2g_transcoder -ldl -lm
#include <stdlib.h>

// GPU Server FFI functions
int xg2g_gpu_server_start(const char* listen_addr, const char* vaapi_device);
int xg2g_gpu_server_stop();
int xg2g_gpu_server_is_running();
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// GPUServer manages the embedded Rust GPU transcoding server.
// The server runs in a separate thread with its own Tokio runtime.
type GPUServer struct {
	listenAddr  string
	vaapiDevice string
	running     bool
}

// NewGPUServer creates a new GPU server instance (not started yet).
func NewGPUServer(listenAddr, vaapiDevice string) *GPUServer {
	return &GPUServer{
		listenAddr:  listenAddr,
		vaapiDevice: vaapiDevice,
		running:     false,
	}
}

// Start starts the embedded GPU transcoding server.
// The server will run in a dedicated thread until Stop() is called.
// Returns an error if the server fails to start or is already running.
func (s *GPUServer) Start() error {
	if s.running {
		return fmt.Errorf("GPU server already running")
	}

	cListenAddr := C.CString(s.listenAddr)
	defer C.free(unsafe.Pointer(cListenAddr))

	cVaapiDevice := C.CString(s.vaapiDevice)
	defer C.free(unsafe.Pointer(cVaapiDevice))

	result := C.xg2g_gpu_server_start(cListenAddr, cVaapiDevice)
	if result != 0 {
		return fmt.Errorf("failed to start GPU server (code %d)", result)
	}

	s.running = true
	return nil
}

// Stop gracefully shuts down the GPU transcoding server.
// This function blocks until the server thread has exited.
// Returns an error if the server is not running or fails to stop.
func (s *GPUServer) Stop() error {
	if !s.running {
		return fmt.Errorf("GPU server not running")
	}

	result := C.xg2g_gpu_server_stop()
	if result != 0 {
		return fmt.Errorf("failed to stop GPU server (code %d)", result)
	}

	s.running = false
	return nil
}

// IsRunning checks if the GPU server is currently running.
func (s *GPUServer) IsRunning() bool {
	result := C.xg2g_gpu_server_is_running()
	return result == 1
}

// GetURL returns the base URL of the GPU transcoding server.
// Example: "http://127.0.0.1:8085"
func (s *GPUServer) GetURL() string {
	return "http://" + s.listenAddr
}
