package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/storageinventory"
)

func TestEvaluateLifecyclePreflightRestoreAcceptsCompleteBackupSet(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	storePath := filepath.Join(dataDir, "store")
	restoreRoot := t.TempDir()
	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storePath,
		},
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKENS=[{\"token\":\"ops-0123456789abcdef0123456789abcdef\",\"scopes\":[\"v3:read\",\"v3:write\"]}]",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})
	createRestoreFixture(t, cfg, restoreRoot, nil)

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})
	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationRestore,
		RuntimeSnapshot: &snapshot,
		RestoreRoot:     restoreRoot,
	})

	if report.Status != LifecyclePreflightSeverityOK || report.Blocking || report.Fatal {
		t.Fatalf("expected ok restore report, got %+v", report)
	}
	if report.Restore == nil || len(report.Restore.Inventory) == 0 {
		t.Fatalf("expected restore assessment inventory, got %+v", report.Restore)
	}
}

func TestEvaluateLifecyclePreflightRestoreBlocksWhenRequiredArtifactMissing(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	storePath := filepath.Join(dataDir, "store")
	restoreRoot := t.TempDir()
	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storePath,
		},
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})
	createRestoreFixture(t, cfg, restoreRoot, map[string]bool{"sessions": true})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})
	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationRestore,
		RuntimeSnapshot: &snapshot,
		RestoreRoot:     restoreRoot,
	})

	if report.Status != LifecyclePreflightSeverityBlock || !report.Blocking {
		t.Fatalf("expected blocking restore report, got %+v", report)
	}
	if !hasLifecycleFinding(report.Findings, "restore.artifact.required_missing", "backup_restore_contract") {
		t.Fatalf("expected missing required restore artifact finding, got %+v", report.Findings)
	}
}

func TestEvaluateLifecyclePreflightRestoreBlocksWhenRuntimeSecretsAreMissing(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	storePath := filepath.Join(dataDir, "store")
	restoreRoot := t.TempDir()
	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storePath,
		},
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
		}, "\n"),
	})
	createRestoreFixture(t, cfg, restoreRoot, nil)

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})
	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationRestore,
		RuntimeSnapshot: &snapshot,
		RestoreRoot:     restoreRoot,
	})

	if report.Status != LifecyclePreflightSeverityBlock || !report.Blocking {
		t.Fatalf("expected blocking restore report, got %+v", report)
	}
	if !hasLifecycleFinding(report.Findings, "restore.external_secret.missing", "backup_restore_contract") {
		t.Fatalf("expected missing restore secret finding, got %+v", report.Findings)
	}
}

func TestEvaluateLifecyclePreflightRestoreFatalsOnForwardIncompatibleSchema(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	storePath := filepath.Join(dataDir, "store")
	restoreRoot := t.TempDir()
	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storePath,
		},
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})
	createRestoreFixture(t, cfg, restoreRoot, nil)
	if err := createSQLiteWithUserVersion(filepath.Join(restoreRoot, "sessions.sqlite"), lifecycleExpectedSQLiteSchemaVersions()["sessions"]+1); err != nil {
		t.Fatalf("create forward incompatible sessions sqlite: %v", err)
	}

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})
	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationRestore,
		RuntimeSnapshot: &snapshot,
		RestoreRoot:     restoreRoot,
	})

	if report.Status != LifecyclePreflightSeverityFatal || !report.Fatal {
		t.Fatalf("expected fatal restore report, got %+v", report)
	}
	if !hasLifecycleFinding(report.Findings, "restore.state_schema.forward_incompatible", "backup_restore_contract") {
		t.Fatalf("expected forward incompatible schema finding, got %+v", report.Findings)
	}
}

func TestEvaluateLifecyclePreflightRestoreWarnsWhenOptionalArtifactMissing(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	storePath := filepath.Join(dataDir, "store")
	restoreRoot := t.TempDir()
	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storePath,
		},
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.6",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})
	createRestoreFixture(t, cfg, restoreRoot, map[string]bool{
		"channels":     true,
		"series_rules": true,
	})

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})
	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationRestore,
		RuntimeSnapshot: &snapshot,
		RestoreRoot:     restoreRoot,
	})

	if report.Status != LifecyclePreflightSeverityWarn || report.Blocking || report.Fatal {
		t.Fatalf("expected warn-only restore report, got %+v", report)
	}
	if !hasLifecycleFinding(report.Findings, "restore.artifact.optional_missing", "backup_restore_contract") {
		t.Fatalf("expected optional restore artifact warning, got %+v", report.Findings)
	}
}

func createRestoreFixture(t *testing.T, cfg config.AppConfig, restoreRoot string, omit map[string]bool) {
	t.Helper()

	expectedVersions := lifecycleExpectedSQLiteSchemaVersions()
	for _, artifact := range lifecycleRestoreArtifacts(cfg) {
		if omit[artifact.ID] {
			continue
		}
		targetPath := filepath.Join(restoreRoot, filepath.Base(artifact.Path))
		switch artifact.Verify {
		case storageinventory.VerifySQLite:
			version := expectedVersions[artifact.ID]
			if err := createSQLiteWithUserVersion(targetPath, version); err != nil {
				t.Fatalf("create %s: %v", artifact.ID, err)
			}
		case storageinventory.VerifyJSON:
			mustWriteRestoreFile(t, targetPath, `{"ok":true}`+"\n")
		default:
			mustWriteRestoreFile(t, targetPath, "placeholder\n")
		}
	}
}

func mustWriteRestoreFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
