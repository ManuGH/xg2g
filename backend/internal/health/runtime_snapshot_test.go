package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestCollectLifecycleRuntimeSnapshotSupportedWhenRuntimeMatches(t *testing.T) {
	installRoot := t.TempDir()
	repoRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)

	writeRuntimeSnapshotFixture(t, installRoot, repoRoot, runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "ghcr.io/manugh/xg2g:v3.4.6",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
		RepoRoot:    repoRoot,
	})

	if snapshot.Drift.Class != LifecycleRuntimeDriftClassSupported {
		t.Fatalf("expected supported runtime drift, got %s with findings %+v", snapshot.Drift.Class, snapshot.Drift.Findings)
	}
	if len(snapshot.Compose.SelectedFiles) != 1 || snapshot.Compose.SelectedFiles[0] != filepath.Join(installRoot, "srv/xg2g/docker-compose.yml") {
		t.Fatalf("unexpected selected compose files: %+v", snapshot.Compose.SelectedFiles)
	}
}

func TestCollectLifecycleRuntimeSnapshotClassifiesComposeImageDriftAsUnsupported(t *testing.T) {
	installRoot := t.TempDir()
	repoRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)

	writeRuntimeSnapshotFixture(t, installRoot, repoRoot, runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.5",
		RepoImage: "ghcr.io/manugh/xg2g:v3.4.6",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
		RepoRoot:    repoRoot,
	})

	if snapshot.Drift.Class != LifecycleRuntimeDriftClassUnsupported {
		t.Fatalf("expected unsupported runtime drift, got %s", snapshot.Drift.Class)
	}
	if !hasRuntimeFinding(snapshot.Drift.Findings, "runtime_snapshot.compose.image_drift") {
		t.Fatalf("expected compose image drift finding, got %+v", snapshot.Drift.Findings)
	}
}

func TestCollectLifecycleRuntimeSnapshotClassifiesExplicitComposeOverrideAsDriftedButAllowed(t *testing.T) {
	installRoot := t.TempDir()
	repoRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)

	writeRuntimeSnapshotFixture(t, installRoot, repoRoot, runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "ghcr.io/manugh/xg2g:v3.4.6",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
			"COMPOSE_FILE=docker-compose.yml:docker-compose.override.yml",
		}, "\n"),
		ExtraInstallFiles: map[string]string{
			"srv/xg2g/docker-compose.override.yml": "services:\n  xg2g: {}\n",
		},
	})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
		RepoRoot:    repoRoot,
	})

	if snapshot.Drift.Class != LifecycleRuntimeDriftClassDriftedButAllowed {
		t.Fatalf("expected drifted_but_allowed runtime drift, got %s with findings %+v", snapshot.Drift.Class, snapshot.Drift.Findings)
	}
	if !hasRuntimeFinding(snapshot.Drift.Findings, "runtime_snapshot.compose.stack_override") {
		t.Fatalf("expected compose stack override finding, got %+v", snapshot.Drift.Findings)
	}
	wantFiles := []string{
		filepath.Join(installRoot, "srv/xg2g/docker-compose.yml"),
		filepath.Join(installRoot, "srv/xg2g/docker-compose.override.yml"),
	}
	if !sameStrings(snapshot.Compose.SelectedFiles, wantFiles) {
		t.Fatalf("unexpected selected compose files: got %+v want %+v", snapshot.Compose.SelectedFiles, wantFiles)
	}
}

func TestCollectLifecycleRuntimeSnapshotBlocksWhenRequiredEnvIsMissing(t *testing.T) {
	installRoot := t.TempDir()
	repoRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)

	writeRuntimeSnapshotFixture(t, installRoot, repoRoot, runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "ghcr.io/manugh/xg2g:v3.4.6",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
		}, "\n"),
	})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
		RepoRoot:    repoRoot,
	})

	if snapshot.Drift.Class != LifecycleRuntimeDriftClassBlocking {
		t.Fatalf("expected blocking runtime drift, got %s", snapshot.Drift.Class)
	}
	if !hasRuntimeFinding(snapshot.Drift.Findings, "runtime_snapshot.env.required_missing") {
		t.Fatalf("expected missing env finding, got %+v", snapshot.Drift.Findings)
	}
	if len(snapshot.Env.MissingRequired) != 1 || snapshot.Env.MissingRequired[0] != "XG2G_DECISION_SECRET|XG2G_PLAYBACK_DECISION_SECRET" {
		t.Fatalf("unexpected missing required keys: %+v", snapshot.Env.MissingRequired)
	}
}

func TestCollectLifecycleRuntimeSnapshotAcceptsScopedTokensAndLegacyDecisionSecret(t *testing.T) {
	installRoot := t.TempDir()
	repoRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)

	writeRuntimeSnapshotFixture(t, installRoot, repoRoot, runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "ghcr.io/manugh/xg2g:v3.4.6",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKENS=[{\"token\":\"ops-0123456789abcdef0123456789abcdef\",\"scopes\":[\"v3:read\",\"v3:write\"]}]",
			"XG2G_PLAYBACK_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
		RepoRoot:    repoRoot,
	})

	if snapshot.Drift.Class != LifecycleRuntimeDriftClassSupported {
		t.Fatalf("expected supported runtime drift, got %s with findings %+v", snapshot.Drift.Class, snapshot.Drift.Findings)
	}
	if len(snapshot.Env.MissingRequired) != 0 {
		t.Fatalf("expected no missing required keys, got %+v", snapshot.Env.MissingRequired)
	}
}

func TestEvaluateLifecyclePreflightIncludesRuntimeTruthFindings(t *testing.T) {
	installRoot := t.TempDir()
	repoRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)

	writeRuntimeSnapshotFixture(t, installRoot, repoRoot, runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.5",
		RepoImage: "ghcr.io/manugh/xg2g:v3.4.6",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
		RepoRoot:    repoRoot,
	})
	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationStartup,
		RuntimeSnapshot: &snapshot,
	})

	if report.Status != LifecyclePreflightSeverityBlock || !report.Blocking {
		t.Fatalf("expected blocking preflight report, got %+v", report)
	}
	if report.Runtime == nil || report.Runtime.Drift.Class != LifecycleRuntimeDriftClassUnsupported {
		t.Fatalf("expected runtime snapshot to be attached, got %+v", report.Runtime)
	}
	if !hasLifecycleFinding(report.Findings, "runtime_snapshot.compose.image_drift", "runtime_truth") {
		t.Fatalf("expected runtime truth finding, got %+v", report.Findings)
	}
}

type runtimeSnapshotFixtureOptions struct {
	DataDir           string
	LiveImage         string
	RepoImage         string
	EnvBody           string
	ExtraInstallFiles map[string]string
}

func runtimeSnapshotTestConfig(dataDir string) config.AppConfig {
	return config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(dataDir, "store"),
		},
	}
}

func writeRuntimeSnapshotFixture(t *testing.T, installRoot, repoRoot string, opts runtimeSnapshotFixtureOptions) {
	t.Helper()

	dataDir := opts.DataDir
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "/var/lib/xg2g"
	}
	unit := "[Unit]\nDescription=xg2g\n[Service]\nEnvironmentFile=/etc/xg2g/xg2g.env\n"
	compose := "services:\n  xg2g:\n    image: " + opts.LiveImage + "\n    env_file:\n      - /etc/xg2g/xg2g.env\n    environment:\n      - XG2G_DATA=" + dataDir + "\n    volumes:\n      - " + dataDir + ":" + dataDir + "\n"
	repoCompose := "services:\n  xg2g:\n    image: " + opts.RepoImage + "\n    env_file:\n      - /etc/xg2g/xg2g.env\n    environment:\n      - XG2G_DATA=" + dataDir + "\n    volumes:\n      - " + dataDir + ":" + dataDir + "\n"

	mustWriteFile(t, filepath.Join(installRoot, "etc/systemd/system/xg2g.service"), unit, 0o644)
	mustWriteFile(t, filepath.Join(installRoot, "srv/xg2g/docs/ops/xg2g.service"), unit, 0o644)
	mustWriteFile(t, filepath.Join(installRoot, "srv/xg2g/docker-compose.yml"), compose, 0o644)
	mustWriteFile(t, filepath.Join(installRoot, "etc/xg2g/xg2g.env"), opts.EnvBody+"\n", 0o600)
	for relPath, body := range opts.ExtraInstallFiles {
		mustWriteFile(t, filepath.Join(installRoot, relPath), body, 0o644)
	}

	if strings.TrimSpace(repoRoot) != "" {
		mustWriteFile(t, filepath.Join(repoRoot, "deploy/xg2g.service"), unit, 0o644)
		mustWriteFile(t, filepath.Join(repoRoot, "deploy/docker-compose.yml"), repoCompose, 0o644)
	}
}

func mustWriteFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func hasRuntimeFinding(findings []LifecycleRuntimeDriftFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func hasLifecycleFinding(findings []LifecyclePreflightFinding, code, contract string) bool {
	for _, finding := range findings {
		if finding.Code == code && finding.Contract == contract {
			return true
		}
	}
	return false
}
