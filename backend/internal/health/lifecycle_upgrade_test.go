package health

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	_ "modernc.org/sqlite"
)

func TestEvaluateLifecyclePreflightUpgradeBlocksWhenConfigMigrationRequired(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)
	fileCfg := config.FileConfig{
		DataDir:       dataDir,
		ConfigVersion: "",
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.5",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})
	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})

	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationUpgrade,
		RuntimeSnapshot: &snapshot,
		FileConfig:      &fileCfg,
		ConfigPath:      filepath.Join(dataDir, "config.yaml"),
		TargetRelease:   "v3.4.6",
	})

	if report.Status != LifecyclePreflightSeverityBlock || !report.Blocking {
		t.Fatalf("expected blocking upgrade report, got %+v", report)
	}
	if report.Upgrade == nil || len(report.Upgrade.ConfigMigrationChanges) == 0 {
		t.Fatalf("expected config migration changes in upgrade assessment, got %+v", report.Upgrade)
	}
	if !hasLifecycleFinding(report.Findings, "upgrade.config_migration.required", "upgrade_migration_contract") {
		t.Fatalf("expected config migration required finding, got %+v", report.Findings)
	}
}

func TestEvaluateLifecyclePreflightUpgradeBlocksWhenTargetIsOlderThanCurrent(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)
	fileCfg := config.FileConfig{
		DataDir:       dataDir,
		ConfigVersion: config.V3ConfigVersion,
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
	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})

	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationUpgrade,
		RuntimeSnapshot: &snapshot,
		FileConfig:      &fileCfg,
		TargetRelease:   "v3.4.5",
	})

	if report.Status != LifecyclePreflightSeverityBlock || !report.Blocking {
		t.Fatalf("expected blocking downgrade report, got %+v", report)
	}
	if !hasLifecycleFinding(report.Findings, "upgrade.release.downgrade_requested", "upgrade_migration_contract") {
		t.Fatalf("expected downgrade finding, got %+v", report.Findings)
	}
}

func TestEvaluateLifecyclePreflightUpgradeBlocksOnDeprecatedSurfaces(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	cfg := runtimeSnapshotTestConfig(dataDir)
	streamPort := 8001
	fileCfg := config.FileConfig{
		DataDir:       dataDir,
		ConfigVersion: config.V3ConfigVersion,
		Enigma2: config.Enigma2Config{
			StreamPort: &streamPort,
		},
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.5",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
			"XG2G_E2_USE_WEBIF_STREAMS=false",
		}, "\n"),
	})
	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})

	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationUpgrade,
		RuntimeSnapshot: &snapshot,
		FileConfig:      &fileCfg,
		TargetRelease:   "v3.4.6",
	})

	if report.Status != LifecyclePreflightSeverityBlock || !report.Blocking {
		t.Fatalf("expected blocking deprecated-surface report, got %+v", report)
	}
	if report.Upgrade == nil || len(report.Upgrade.DeprecatedSurfaces) < 2 {
		t.Fatalf("expected deprecated surfaces in upgrade assessment, got %+v", report.Upgrade)
	}
	if !hasLifecycleFinding(report.Findings, "upgrade.runtime_env.deprecated_key", "upgrade_migration_contract") {
		t.Fatalf("expected deprecated env key finding, got %+v", report.Findings)
	}
	if !hasLifecycleFinding(report.Findings, "upgrade.config.deprecated_path", "upgrade_migration_contract") {
		t.Fatalf("expected deprecated config path finding, got %+v", report.Findings)
	}
}

func TestEvaluateLifecyclePreflightUpgradeWarnsWhenStateMigrationIsRequired(t *testing.T) {
	installRoot := t.TempDir()
	dataDir := t.TempDir()
	storePath := filepath.Join(dataDir, "store")
	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storePath,
		},
	}
	fileCfg := config.FileConfig{
		DataDir:       dataDir,
		ConfigVersion: config.V3ConfigVersion,
	}

	writeRuntimeSnapshotFixture(t, installRoot, "", runtimeSnapshotFixtureOptions{
		DataDir:   dataDir,
		LiveImage: "ghcr.io/manugh/xg2g:v3.4.5",
		RepoImage: "",
		EnvBody: strings.Join([]string{
			"XG2G_E2_HOST=http://receiver.local",
			"XG2G_API_TOKEN=token-0123456789",
			"XG2G_DECISION_SECRET=decision-secret-0123456789abcdef",
		}, "\n"),
	})
	if err := createSQLiteWithUserVersion(runtimeJoin(installRoot, filepath.Join(storePath, "sessions.sqlite")), 4); err != nil {
		t.Fatalf("create sessions sqlite: %v", err)
	}

	snapshot := CollectLifecycleRuntimeSnapshot(context.Background(), cfg, LifecycleRuntimeSnapshotOptions{
		InstallRoot: installRoot,
	})
	report := EvaluateLifecyclePreflight(context.Background(), cfg, LifecyclePreflightOptions{
		Operation:       LifecycleOperationUpgrade,
		RuntimeSnapshot: &snapshot,
		FileConfig:      &fileCfg,
		TargetRelease:   "v3.4.6",
	})

	if report.Status != LifecyclePreflightSeverityWarn || report.Blocking || report.Fatal {
		t.Fatalf("expected warn-only upgrade report, got %+v", report)
	}
	if report.Upgrade == nil || len(report.Upgrade.StateSchemas) == 0 {
		t.Fatalf("expected state schema assessment, got %+v", report.Upgrade)
	}
	if !hasLifecycleFinding(report.Findings, "upgrade.state_schema.migration_required", "upgrade_migration_contract") {
		t.Fatalf("expected schema migration warning, got %+v", report.Findings)
	}
}

func createSQLiteWithUserVersion(path string, version int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS test (id INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA user_version = " + strconv.Itoa(version)); err != nil {
		return err
	}
	return nil
}
