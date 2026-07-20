// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeFileGovernance ensures the root-level governance keys
// (configStrict/readyStrict/logService/trustedProxies) set in YAML are actually
// applied. They were parsed into FileConfig but never merged, so file values were
// silently ignored.
func TestMergeFileGovernance(t *testing.T) {
	setRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	setRequiredTestSecrets(t)
	// Ensure no ENV override leaks in (ENV > File).
	for _, k := range []string{"XG2G_CONFIG_STRICT", "XG2G_READY_STRICT", "XG2G_LOG_SERVICE", "XG2G_TRUSTED_PROXIES"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := strings.TrimSpace(`
enigma2:
  baseUrl: "http://receiver.example.com"
configStrict: false
readyStrict: true
logService: "xg2g-edge"
trustedProxies: "10.0.0.0/8"
`)
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := NewLoader(path, "dev").Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ConfigStrict != false {
		t.Errorf("configStrict: got %v, want false (YAML ignored)", cfg.ConfigStrict)
	}
	if cfg.ReadyStrict != true {
		t.Errorf("readyStrict: got %v, want true (YAML ignored)", cfg.ReadyStrict)
	}
	if cfg.LogService != "xg2g-edge" {
		t.Errorf("logService: got %q, want %q (YAML ignored)", cfg.LogService, "xg2g-edge")
	}
	if cfg.TrustedProxies != "10.0.0.0/8" {
		t.Errorf("trustedProxies: got %q, want %q (YAML ignored)", cfg.TrustedProxies, "10.0.0.0/8")
	}
}
