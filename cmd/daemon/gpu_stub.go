//go:build !gpu
// +build !gpu

// SPDX-License-Identifier: MIT
package main

// initGPUServer is a no-op when GPU support is not compiled in
func initGPUServer() interface{} {
	return nil
}
