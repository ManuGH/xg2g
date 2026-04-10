package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPreflightCLIRejectsUnknownOperation(t *testing.T) {
	if code := runPreflightCLIWithIO([]string{"--operation", "unknown"}, &bytes.Buffer{}, &bytes.Buffer{}, nil); code != 2 {
		t.Fatalf("runPreflightCLI(unknown operation) = %d, want 2", code)
	}
}

func TestRunPreflightCLIRunsAgainstExplicitConfig(t *testing.T) {
	clearXG2GEnv(t)
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.yaml")
	configYAML := "dataDir: " + dataDir + "\n" +
		"enigma2:\n" +
		"  baseUrl: http://127.0.0.1\n" +
		"engine:\n" +
		"  enabled: false\n" +
		"store:\n" +
		"  backend: memory\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("XG2G_STORE_BACKEND", "memory")
	t.Setenv("XG2G_STORE_PATH", filepath.Join(dataDir, "store"))

	if code := runPreflightCLIWithIO([]string{"--config", configPath, "--json"}, &bytes.Buffer{}, &bytes.Buffer{}, nil); code != 0 {
		t.Fatalf("runPreflightCLI(--config %s --json) = %d, want 0", configPath, code)
	}
}

func TestRunPreflightCLIIncludesRuntimeSnapshotJSON(t *testing.T) {
	clearXG2GEnv(t)
	configDir := t.TempDir()
	installRoot := t.TempDir()
	repoRoot := t.TempDir()
	dataDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	configYAML := "dataDir: " + dataDir + "\n" +
		"enigma2:\n" +
		"  baseUrl: http://127.0.0.1\n" +
		"engine:\n" +
		"  enabled: false\n" +
		"store:\n" +
		"  backend: memory\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("XG2G_STORE_BACKEND", "memory")
	t.Setenv("XG2G_STORE_PATH", filepath.Join(dataDir, "store"))
	writePreflightRuntimeFixture(t, installRoot, repoRoot, dataDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runPreflightCLIWithIO([]string{
		"--config", configPath,
		"--json",
		"--runtime-snapshot",
		"--install-root", installRoot,
		"--repo-root", repoRoot,
	}, &stdout, &stderr, func() string { return repoRoot })

	if code != 1 {
		t.Fatalf("expected exit 1 for unsupported runtime drift, got %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"runtime"`) {
		t.Fatalf("expected runtime snapshot JSON, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"class": "unsupported"`) {
		t.Fatalf("expected unsupported runtime drift in JSON, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"runtime_snapshot.compose.image_drift"`) {
		t.Fatalf("expected compose image drift finding in JSON, got %q", stdout.String())
	}
}

func TestRunPreflightCLIRestoreIncludesAssessmentJSON(t *testing.T) {
	clearXG2GEnv(t)
	configDir := t.TempDir()
	installRoot := t.TempDir()
	restoreRoot := t.TempDir()
	dataDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	configYAML := "dataDir: " + dataDir + "\n" +
		"enigma2:\n" +
		"  baseUrl: http://127.0.0.1\n" +
		"engine:\n" +
		"  enabled: false\n" +
		"store:\n" +
		"  backend: sqlite\n" +
		"  path: " + filepath.Join(dataDir, "store") + "\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("XG2G_STORE_BACKEND", "sqlite")
	t.Setenv("XG2G_STORE_PATH", filepath.Join(dataDir, "store"))
	writePreflightRuntimeFixture(t, installRoot, "", dataDir)
	if err := os.WriteFile(filepath.Join(restoreRoot, "sessions.sqlite"), []byte("not a sqlite database"), 0o644); err != nil {
		t.Fatalf("write restore sessions fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runPreflightCLIWithIO([]string{
		"--config", configPath,
		"--operation", "restore",
		"--json",
		"--runtime-snapshot",
		"--install-root", installRoot,
		"--restore-root", restoreRoot,
	}, &stdout, &stderr, nil)

	if code == 0 {
		t.Fatalf("expected non-zero restore preflight exit, got %d", code)
	}
	if !strings.Contains(stdout.String(), `"restore"`) {
		t.Fatalf("expected restore assessment JSON, got %q", stdout.String())
	}
}

func writePreflightRuntimeFixture(t *testing.T, installRoot, repoRoot, dataDir string) {
	t.Helper()

	unit := "[Unit]\nDescription=xg2g\n[Service]\nEnvironmentFile=/etc/xg2g/xg2g.env\n"
	liveCompose := "services:\n  xg2g:\n    image: ghcr.io/manugh/xg2g:v3.4.5\n    env_file:\n      - /etc/xg2g/xg2g.env\n    environment:\n      - XG2G_DATA=" + dataDir + "\n    volumes:\n      - " + dataDir + ":" + dataDir + "\n"
	repoCompose := "services:\n  xg2g:\n    image: ghcr.io/manugh/xg2g:v3.4.6\n    env_file:\n      - /etc/xg2g/xg2g.env\n    environment:\n      - XG2G_DATA=" + dataDir + "\n    volumes:\n      - " + dataDir + ":" + dataDir + "\n"
	envBody := strings.Join([]string{
		"XG2G_E2_HOST=http://receiver.local",
		"XG2G_API_TOKEN=token-0123456789",
		"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
	}, "\n") + "\n"

	mustWritePreflightFile(t, filepath.Join(installRoot, "etc/systemd/system/xg2g.service"), unit)
	mustWritePreflightFile(t, filepath.Join(installRoot, "srv/xg2g/docs/ops/xg2g.service"), unit)
	mustWritePreflightFile(t, filepath.Join(installRoot, "srv/xg2g/docker-compose.yml"), liveCompose)
	if err := os.MkdirAll(filepath.Join(installRoot, "etc/xg2g"), 0o755); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "etc/xg2g/xg2g.env"), []byte(envBody), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if repoRoot != "" {
		mustWritePreflightFile(t, filepath.Join(repoRoot, "deploy/xg2g.service"), unit)
		mustWritePreflightFile(t, filepath.Join(repoRoot, "deploy/docker-compose.yml"), repoCompose)
	}
}

func mustWritePreflightFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func clearXG2GEnv(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(key, "XG2G_") {
			t.Setenv(key, "")
		}
	}
}
