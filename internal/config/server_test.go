// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"testing"
	"time"
)

func TestParseServerConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(*testing.T, ServerConfig)
	}{
		{
			name:    "defaults when no env vars set",
			envVars: map[string]string{},
			validate: func(t *testing.T, cfg ServerConfig) {
				t.Helper()
				if cfg.ListenAddr != ":8080" {
					t.Errorf("ListenAddr = %v, want :8080", cfg.ListenAddr)
				}
				if cfg.ReadTimeout != 60*time.Second {
					t.Errorf("ReadTimeout = %v, want 60s", cfg.ReadTimeout)
				}
				if cfg.WriteTimeout != 0 {
					t.Errorf("WriteTimeout = %v, want 0s", cfg.WriteTimeout)
				}
				if cfg.IdleTimeout != 120*time.Second {
					t.Errorf("IdleTimeout = %v, want 120s", cfg.IdleTimeout)
				}
				if cfg.MaxHeaderBytes != 1<<20 {
					t.Errorf("MaxHeaderBytes = %v, want %v", cfg.MaxHeaderBytes, 1<<20)
				}
				if cfg.ShutdownTimeout != 15*time.Second {
					t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
				}
			},
		},
		{
			name: "custom values from env vars",
			envVars: map[string]string{
				"XG2G_LISTEN":                  ":9000",
				"XG2G_SERVER_READ_TIMEOUT":     "10s",
				"XG2G_SERVER_WRITE_TIMEOUT":    "20s",
				"XG2G_SERVER_IDLE_TIMEOUT":     "300s",
				"XG2G_SERVER_MAX_HEADER_BYTES": "2097152",
				"XG2G_SERVER_SHUTDOWN_TIMEOUT": "30s",
			},
			validate: func(t *testing.T, cfg ServerConfig) {
				t.Helper()
				if cfg.ListenAddr != ":9000" {
					t.Errorf("ListenAddr = %v, want :9000", cfg.ListenAddr)
				}
				if cfg.ReadTimeout != 10*time.Second {
					t.Errorf("ReadTimeout = %v, want 10s", cfg.ReadTimeout)
				}
				if cfg.WriteTimeout != 20*time.Second {
					t.Errorf("WriteTimeout = %v, want 20s", cfg.WriteTimeout)
				}
				if cfg.IdleTimeout != 300*time.Second {
					t.Errorf("IdleTimeout = %v, want 300s", cfg.IdleTimeout)
				}
				if cfg.MaxHeaderBytes != 2097152 {
					t.Errorf("MaxHeaderBytes = %v, want 2097152", cfg.MaxHeaderBytes)
				}
				if cfg.ShutdownTimeout != 30*time.Second {
					t.Errorf("ShutdownTimeout = %v, want 30s", cfg.ShutdownTimeout)
				}
			},
		},
		{
			name: "listen addr from alias",
			envVars: map[string]string{
				"XG2G_API_ADDR": ":9100",
			},
			validate: func(t *testing.T, cfg ServerConfig) {
				t.Helper()
				if cfg.ListenAddr != ":9100" {
					t.Errorf("ListenAddr = %v, want :9100", cfg.ListenAddr)
				}
			},
		},
		{
			name: "invalid values fall back to defaults",
			envVars: map[string]string{
				"XG2G_SERVER_READ_TIMEOUT":     "invalid",
				"XG2G_SERVER_MAX_HEADER_BYTES": "not-a-number",
			},
			validate: func(t *testing.T, cfg ServerConfig) {
				t.Helper()
				if cfg.ReadTimeout != 60*time.Second {
					t.Errorf("ReadTimeout = %v, want 60s (default)", cfg.ReadTimeout)
				}
				if cfg.WriteTimeout != 0 {
					t.Errorf("WriteTimeout = %v, want 0s (default)", cfg.WriteTimeout)
				}
				if cfg.IdleTimeout != 120*time.Second {
					t.Errorf("IdleTimeout = %v, want 120s (default)", cfg.IdleTimeout)
				}
				if cfg.MaxHeaderBytes != 1<<20 {
					t.Errorf("MaxHeaderBytes = %v, want %v (default)", cfg.MaxHeaderBytes, 1<<20)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment
			for key, value := range tt.envVars {
				_ = os.Setenv(key, value)
				defer func(k string) { _ = os.Unsetenv(k) }(key)
			}

			// Test
			cfg := ParseServerConfig()
			tt.validate(t, cfg)
		})
	}
}

func TestParseMetricsAddr(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		envSet   bool
		want     string
	}{
		{
			name:   "metrics disabled (default)",
			envSet: false,
			want:   "",
		},
		{
			name:     "metrics enabled with custom port",
			envValue: ":9090",
			envSet:   true,
			want:     ":9090",
		},
		{
			name:     "metrics enabled with full address",
			envValue: "localhost:9091",
			envSet:   true,
			want:     "localhost:9091",
		},
		{
			name:     "empty string disables metrics",
			envValue: "",
			envSet:   true,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			key := "XG2G_METRICS_LISTEN"
			if tt.envSet {
				_ = os.Setenv(key, tt.envValue)
				defer func() { _ = os.Unsetenv(key) }()
			}

			// Test
			got := ParseMetricsAddr()
			if got != tt.want {
				t.Errorf("ParseMetricsAddr() = %v, want %v", got, tt.want)
			}
		})
	}
}
