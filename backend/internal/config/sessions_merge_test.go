// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"
	"time"
)

// TestSessionsConfigDefaultLowered guards the leak window: a one-heartbeat-then-crash
// client extends its session-record expiry by sessions.lease_ttl (sessions_heartbeat.go),
// so the old 2h default pinned a tuner/pipeline slot for 2h. Default must now be 120s.
func TestSessionsConfigDefaultLowered(t *testing.T) {
	SetRequiredTestSecrets(t)
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults: %v", err)
	}
	if cfg.Sessions.LeaseTTL != 120*time.Second {
		t.Errorf("default sessions.lease_ttl = %v, want 120s", cfg.Sessions.LeaseTTL)
	}
	if cfg.Sessions.HeartbeatInterval != 30*time.Second {
		t.Errorf("default sessions.heartbeat_interval = %v, want 30s", cfg.Sessions.HeartbeatInterval)
	}
	// Invariant the lease design relies on: a live client must be able to renew
	// before expiry (heartbeat interval strictly below lease TTL).
	if !(cfg.Sessions.HeartbeatInterval < cfg.Sessions.LeaseTTL) {
		t.Errorf("heartbeat_interval (%v) must be < lease_ttl (%v)", cfg.Sessions.HeartbeatInterval, cfg.Sessions.LeaseTTL)
	}
}

// TestSessionsConfigFileOverride proves sessions.* YAML is honoured (mergeFileSessions);
// before this it was a dead no-op.
func TestSessionsConfigFileOverride(t *testing.T) {
	SetRequiredTestSecrets(t)
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults: %v", err)
	}
	src := &FileConfig{Sessions: &SessionsConfig{LeaseTTL: 5 * time.Minute, HeartbeatInterval: 20 * time.Second}}
	loader.mergeFileSessions(&cfg, src)

	if cfg.Sessions.LeaseTTL != 5*time.Minute {
		t.Errorf("file lease_ttl = %v, want 5m", cfg.Sessions.LeaseTTL)
	}
	if cfg.Sessions.HeartbeatInterval != 20*time.Second {
		t.Errorf("file heartbeat_interval = %v, want 20s", cfg.Sessions.HeartbeatInterval)
	}
	// A field absent from YAML must retain the default.
	if cfg.Sessions.ExpiryCheckInterval != 1*time.Minute {
		t.Errorf("unset expiry_check_interval = %v, want default 1m", cfg.Sessions.ExpiryCheckInterval)
	}
}

// TestSessionsConfigEnvOverride proves XG2G_SESSION_* env wins over file/default.
func TestSessionsConfigEnvOverride(t *testing.T) {
	SetRequiredTestSecrets(t)
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults: %v", err)
	}
	t.Setenv("XG2G_SESSION_LEASE_TTL", "45s")
	t.Setenv("XG2G_SESSION_HEARTBEAT_INTERVAL", "15s")
	t.Setenv("XG2G_SESSION_EXPIRY_CHECK_INTERVAL", "90s")
	loader.mergeEnvConfig(&cfg)

	if cfg.Sessions.LeaseTTL != 45*time.Second {
		t.Errorf("env lease_ttl = %v, want 45s", cfg.Sessions.LeaseTTL)
	}
	if cfg.Sessions.HeartbeatInterval != 15*time.Second {
		t.Errorf("env heartbeat_interval = %v, want 15s", cfg.Sessions.HeartbeatInterval)
	}
	if cfg.Sessions.ExpiryCheckInterval != 90*time.Second {
		t.Errorf("env expiry_check_interval = %v, want 90s", cfg.Sessions.ExpiryCheckInterval)
	}
}
