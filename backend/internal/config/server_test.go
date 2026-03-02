// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

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
				if cfg.ListenAddr != ":8088" {
					t.Errorf("ListenAddr = %v, want :8088", cfg.ListenAddr)
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

func TestParseServerConfigForApp(t *testing.T) {
	t.Run("yaml listen addr when env is unset", func(t *testing.T) {
		t.Setenv("XG2G_LISTEN", "")
		cfg := AppConfig{APIListenAddr: ":7777"}
		got := ParseServerConfigForApp(cfg)
		if got.ListenAddr != ":7777" {
			t.Fatalf("ListenAddr = %q, want %q", got.ListenAddr, ":7777")
		}
	})

	t.Run("env listen addr overrides yaml", func(t *testing.T) {
		t.Setenv("XG2G_LISTEN", ":9999")
		cfg := AppConfig{APIListenAddr: ":7777"}
		got := ParseServerConfigForApp(cfg)
		if got.ListenAddr != ":9999" {
			t.Fatalf("ListenAddr = %q, want %q", got.ListenAddr, ":9999")
		}
	})

	t.Run("registry default when both env and yaml are empty", func(t *testing.T) {
		t.Setenv("XG2G_LISTEN", "")
		cfg := AppConfig{}
		got := ParseServerConfigForApp(cfg)
		if got.ListenAddr != ":8088" {
			t.Fatalf("ListenAddr = %q, want %q", got.ListenAddr, ":8088")
		}
	})

	t.Run("yaml server defaults are honored when env is unset", func(t *testing.T) {
		t.Setenv("XG2G_SERVER_READ_TIMEOUT", "")
		t.Setenv("XG2G_SERVER_WRITE_TIMEOUT", "")
		t.Setenv("XG2G_SERVER_IDLE_TIMEOUT", "")
		t.Setenv("XG2G_SERVER_MAX_HEADER_BYTES", "")
		t.Setenv("XG2G_SERVER_SHUTDOWN_TIMEOUT", "")
		cfg := AppConfig{
			Server: ServerRuntimeConfig{
				ReadTimeout:     11 * time.Second,
				WriteTimeout:    22 * time.Second,
				IdleTimeout:     33 * time.Second,
				MaxHeaderBytes:  1234,
				ShutdownTimeout: 44 * time.Second,
			},
		}
		got := ParseServerConfigForApp(cfg)
		if got.ReadTimeout != 11*time.Second {
			t.Fatalf("ReadTimeout = %v, want %v", got.ReadTimeout, 11*time.Second)
		}
		if got.WriteTimeout != 22*time.Second {
			t.Fatalf("WriteTimeout = %v, want %v", got.WriteTimeout, 22*time.Second)
		}
		if got.IdleTimeout != 33*time.Second {
			t.Fatalf("IdleTimeout = %v, want %v", got.IdleTimeout, 33*time.Second)
		}
		if got.MaxHeaderBytes != 1234 {
			t.Fatalf("MaxHeaderBytes = %d, want %d", got.MaxHeaderBytes, 1234)
		}
		if got.ShutdownTimeout != 44*time.Second {
			t.Fatalf("ShutdownTimeout = %v, want %v", got.ShutdownTimeout, 44*time.Second)
		}
	})

	t.Run("env server values override yaml server values", func(t *testing.T) {
		t.Setenv("XG2G_SERVER_READ_TIMEOUT", "13s")
		t.Setenv("XG2G_SERVER_WRITE_TIMEOUT", "14s")
		t.Setenv("XG2G_SERVER_IDLE_TIMEOUT", "15s")
		t.Setenv("XG2G_SERVER_MAX_HEADER_BYTES", "16384")
		t.Setenv("XG2G_SERVER_SHUTDOWN_TIMEOUT", "16s")
		cfg := AppConfig{
			Server: ServerRuntimeConfig{
				ReadTimeout:     1 * time.Second,
				WriteTimeout:    2 * time.Second,
				IdleTimeout:     3 * time.Second,
				MaxHeaderBytes:  1000,
				ShutdownTimeout: 4 * time.Second,
			},
		}
		got := ParseServerConfigForApp(cfg)
		if got.ReadTimeout != 13*time.Second {
			t.Fatalf("ReadTimeout = %v, want %v", got.ReadTimeout, 13*time.Second)
		}
		if got.WriteTimeout != 14*time.Second {
			t.Fatalf("WriteTimeout = %v, want %v", got.WriteTimeout, 14*time.Second)
		}
		if got.IdleTimeout != 15*time.Second {
			t.Fatalf("IdleTimeout = %v, want %v", got.IdleTimeout, 15*time.Second)
		}
		if got.MaxHeaderBytes != 16384 {
			t.Fatalf("MaxHeaderBytes = %d, want %d", got.MaxHeaderBytes, 16384)
		}
		if got.ShutdownTimeout != 16*time.Second {
			t.Fatalf("ShutdownTimeout = %v, want %v", got.ShutdownTimeout, 16*time.Second)
		}
	})
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
