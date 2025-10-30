//go:build gpu
// +build gpu

// SPDX-License-Identifier: MIT
package main

import (
	"github.com/ManuGH/xg2g/internal/config"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/transcoder"
)

// initGPUServer initializes and starts the GPU transcoding server if enabled
// Returns interface{} to match gpu_stub.go and allow type assertion in main.go
func initGPUServer() interface{} {
	if config.ParseString("XG2G_ENABLE_GPU_TRANSCODING", "false") != "true" {
		return nil
	}

	logger := xglog.WithComponent("daemon")
	gpuListenAddr := config.ParseString("XG2G_GPU_LISTEN", "127.0.0.1:8085")
	vaapiDevice := config.ParseString("XG2G_VAAPI_DEVICE", "/dev/dri/renderD128")

	gpuServer := transcoder.NewGPUServer(gpuListenAddr, vaapiDevice)

	logger.Info().
		Str("listen", gpuListenAddr).
		Str("vaapi_device", vaapiDevice).
		Msg("Starting embedded GPU transcoding server (MODE 3)")

	if err := gpuServer.Start(); err != nil {
		logger.Fatal().
			Err(err).
			Str("event", "gpu.start.failed").
			Msg("failed to start GPU transcoding server")
	}

	logger.Info().
		Str("url", gpuServer.GetURL()).
		Msg("GPU transcoding server started successfully")

	return gpuServer
}
