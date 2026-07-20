package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeVODTestConfig writes a minimal valid config with an optional vod: block appended.
func writeVODTestConfig(t *testing.T, vodBlock string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "dataDir: " + dir + "\n" +
		"enigma2:\n" +
		"  baseUrl: http://box.local\n" +
		"  username: u\n" +
		"  password: p\n" +
		"api:\n" +
		"  token: tok\n" +
		"  tokenScopes:\n" +
		"    - v3:read\n" +
		vodBlock
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// M14 — RED CONTROL (merge part): the one runtime-observable guarantee. XG2G_VOD_CACHE_TTL
// is the only env-configurable VOD field with a runtime reader today (the recording-cache
// eviction loop). env-only (no YAML vod:) must reach the flat field the runtime reads. Goes
// RED if mergeEnvVOD is missing / merges the wrong key (cfg.VODCacheTTL would stay 0).
func TestVODEnvMerge_EnvOnlyCacheTTLReachesRuntimeField(t *testing.T) {
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_VOD_CACHE_TTL", "12h")

	cfg, err := NewLoader(writeVODTestConfig(t, ""), "test").Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.VODCacheTTL != 12*time.Hour {
		t.Fatalf("env-only XG2G_VOD_CACHE_TTL must reach cfg.VODCacheTTL (the runtime-read field); got %v, want 12h", cfg.VODCacheTTL)
	}
}

// M14 — CONTRACT guard (NOT a runtime effect). VODProbeSize has no runtime reader today (the
// real probe params come from cfg.Enigma2.*; see finding M14c). This pins the merge contract
// (env -> config field, per-key) only — a green check here does NOT mean the value does
// anything at runtime.
func TestVODEnvMerge_ProbeSizeMergeContractOnly(t *testing.T) {
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_VOD_PROBE_SIZE", "99M")

	cfg, err := NewLoader(writeVODTestConfig(t, ""), "test").Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.VODProbeSize != "99M" {
		t.Fatalf("merge contract: env XG2G_VOD_PROBE_SIZE should populate cfg.VODProbeSize; got %q", cfg.VODProbeSize)
	}
}

// Governance: two EFFECTIVE sources that differ must fail-closed (not silently let env win).
func TestVODEnvMerge_ConflictWhenBothSetAndDiffer(t *testing.T) {
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_VOD_CACHE_TTL", "12h")

	_, err := NewLoader(writeVODTestConfig(t, "vod:\n  cacheTTL: \"1h\"\n"), "test").Load()
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("YAML + env both effective and differ must fail-closed; got err=%v", err)
	}
}

// Equal values are not a conflict; the (identical) effective value applies.
func TestVODEnvMerge_NoConflictWhenEqual(t *testing.T) {
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_VOD_CACHE_TTL", "12h")

	cfg, err := NewLoader(writeVODTestConfig(t, "vod:\n  cacheTTL: \"12h\"\n"), "test").Load()
	if err != nil {
		t.Fatalf("equal values must not conflict: %v", err)
	}
	if cfg.VODCacheTTL != 12*time.Hour {
		t.Fatalf("got %v, want 12h", cfg.VODCacheTTL)
	}
}

// M14 — RED CONTROL (conflict part): an EMPTY env var must not phantom-conflict with a
// non-empty file value. The merge ignores an empty var (envPresent), so the check must too —
// a value that is never applied cannot collide with anything. Goes RED if checkVODConflicts
// stops sharing envPresent and reintroduces an `ok`-based (empty==set) presence test.
func TestVODEnvMerge_EmptyEnvDoesNotPhantomConflict(t *testing.T) {
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_VOD_PROBE_SIZE", "") // explicitly empty

	cfg, err := NewLoader(writeVODTestConfig(t, "vod:\n  probeSize: \"10M\"\n"), "test").Load()
	if err != nil {
		t.Fatalf("empty env var must not phantom-conflict with a file value: %v", err)
	}
	if cfg.VODProbeSize != "10M" {
		t.Fatalf("empty env must not override the file value; got %q, want 10M", cfg.VODProbeSize)
	}
}

// Empty on BOTH sides (env empty, YAML omits the field) is consistently "not set" → no
// conflict. Pins that empty counts as unset on the file side too, not just the env side.
func TestVODEnvMerge_BothEmptyNoConflict(t *testing.T) {
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_VOD_PROBE_SIZE", "")

	// vod: present (so the check runs) but probeSize omitted -> file side also not effective.
	if _, err := NewLoader(writeVODTestConfig(t, "vod:\n  cacheTTL: \"24h\"\n"), "test").Load(); err != nil {
		t.Fatalf("empty on both sides must not conflict: %v", err)
	}
}

// Symmetric to the empty-env case: env set + YAML field empty/omitted -> env wins, no
// conflict (the file side has nothing effective). Guards that the empty check is symmetric.
func TestVODEnvMerge_EnvSetYAMLEmptyEnvWins(t *testing.T) {
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_VOD_CACHE_TTL", "12h")

	// vod: present, cacheTTL omitted -> file side empty for cacheTTL.
	cfg, err := NewLoader(writeVODTestConfig(t, "vod:\n  probeSize: \"10M\"\n"), "test").Load()
	if err != nil {
		t.Fatalf("env set + YAML field empty must not conflict: %v", err)
	}
	if cfg.VODCacheTTL != 12*time.Hour {
		t.Fatalf("env should win for the file-empty field; got %v, want 12h", cfg.VODCacheTTL)
	}
}
