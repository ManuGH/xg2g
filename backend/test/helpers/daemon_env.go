// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package helpers

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestRecordingsSigningKey satisfies the >=32 character requirement the daemon
// enforces on recordings.target_signing_key.
const TestRecordingsSigningKey = "test-recordings-target-signing-key-0123456789"

// TestAPIToken is the bearer token a daemon started via DaemonEnv accepts
// unless the caller overrides XG2G_API_TOKEN. The daemon refuses to start with
// no token configured at all.
const TestAPIToken = "test-api-token"

// DaemonEnv returns the environment for a spawned xg2g daemon: the minimum the
// config validator and service wiring refuse to start without, plus whatever
// the caller passes in extra. Later entries win, so a test can override any
// baseline key (e.g. XG2G_API_TOKEN) by passing its own.
//
// Every key in the baseline is one the daemon requires unconditionally. When
// the daemon grows another such requirement, adding it here fixes all spawning
// tests at once — previously each one hand-rolled its environment, so a new
// requirement broke them all and nobody noticed because none of them ran.
//
// The result replaces the process environment (exec.Cmd.Env semantics), so PATH
// is carried over explicitly for tests that shell out to ffmpeg.
func DaemonEnv(t *testing.T, dataDir string, port int, extra ...string) []string {
	t.Helper()

	if dataDir == "" {
		dataDir = t.TempDir()
	}
	storeDir := filepath.Join(dataDir, "store")
	if err := os.MkdirAll(storeDir, 0o750); err != nil {
		t.Fatalf("create store dir: %v", err)
	}

	base := []string{
		"XG2G_DATA=" + dataDir,
		"XG2G_STORE_PATH=" + storeDir,
		fmt.Sprintf("XG2G_LISTEN=:%d", port),
		"XG2G_DECISION_SECRET=" + TestDecisionSecret,
		"XG2G_RECORDINGS_TARGET_SIGNING_KEY=" + TestRecordingsSigningKey,
		"XG2G_API_TOKEN=" + TestAPIToken,
		"XG2G_API_TOKEN_SCOPES=v3:read,v3:write",
		"PATH=" + os.Getenv("PATH"),
	}

	return mergeEnv(base, extra)
}

// mergeEnv folds overrides into base, last assignment per key wins, and returns
// a deterministically ordered slice. Duplicate keys in an exec environment are
// resolved inconsistently across platforms, so collapse them here instead.
func mergeEnv(base, overrides []string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, pair := range append(append([]string{}, base...), overrides...) {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		merged[key] = value
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}
	return env
}
