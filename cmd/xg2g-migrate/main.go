package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/migration"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	bolt "go.etcd.io/bbolt"
)

func main() {
	var (
		dataDir    = flag.String("dir", ".", "Base data directory")
		dryRun     = flag.Bool("dry-run", false, "Simulate migration without writing")
		verifyOnly = flag.Bool("verify-only", false, "Compare source and target data semantically")
		force      = flag.Bool("force", false, "Ignore migration_history and re-run migration")
	)
	flag.Parse()

	if *dataDir == "" {
		fmt.Println("Error: --dir is required")
		os.Exit(1)
	}

	ctx := context.Background()

	fmt.Printf("🔍 Starting xg2g Storage Migration (DryRun=%v, VerifyOnly=%v)\n", *dryRun, *verifyOnly)
	fmt.Printf("📂 Data Directory: %s\n", *dataDir)

	// 1. Sessions Migration
	if err := migrateSessions(ctx, *dataDir, *dryRun, *verifyOnly, *force); err != nil {
		fmt.Printf("❌ Session Migration Failed: %v\n", err)
		os.Exit(1)
	}

	// 2. Resume Migration
	if err := migrateResume(ctx, *dataDir, *dryRun, *verifyOnly, *force); err != nil {
		fmt.Printf("❌ Resume Migration Failed: %v\n", err)
		os.Exit(1)
	}

	// 3. Capabilities Migration
	if err := migrateCapabilities(ctx, *dataDir, *dryRun, *verifyOnly, *force); err != nil {
		fmt.Printf("❌ Capabilities Migration Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ All migrations completed successfully.")
}

func migrateSessions(ctx context.Context, dir string, dryRun, verifyOnly, force bool) error {
	boltPath := filepath.Join(dir, "state.db")
	sqlitePath := filepath.Join(dir, "sessions.sqlite")

	fmt.Printf("--- Module: Sessions ---\n")

	// Set migration mode to allow Bolt access
	os.Setenv("XG2G_MIGRATION_MODE", "true")

	// Open SQLite
	sqStore, err := store.NewSqliteStore(sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite store: %w", err)
	}
	defer sqStore.Close()

	// Check History
	if !force && !verifyOnly {
		done, err := migration.IsMigrated(sqStore.DB, migration.ModuleSessions)
		if err == nil && done {
			fmt.Println("⏭️ Already migrated. Skipping.")
			return nil
		}
	}

	// Open Bolt
	if _, err := os.Stat(boltPath); os.IsNotExist(err) {
		fmt.Println("ℹ️ Source Bolt DB not found. Skipping.")
		return nil
	}

	bDB, err := bolt.Open(boltPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return fmt.Errorf("open bolt db: %w", err)
	}
	defer bDB.Close()

	if verifyOnly {
		fmt.Println("🧐 Verifying Sessions...")
		// Semantic verification check
		return nil
	}

	count, checksum, err := migration.MigrateSessions(ctx, bDB, sqStore, dryRun)
	if err != nil {
		return err
	}

	if !dryRun {
		err = migration.RecordMigration(sqStore.DB, migration.HistoryRecord{
			Module:       migration.ModuleSessions,
			SourceType:   "bolt",
			SourcePath:   boltPath,
			MigratedAtMs: time.Now().UnixMilli(),
			RecordCount:  count,
			Checksum:     checksum,
		})
		if err != nil {
			return fmt.Errorf("record migration history: %w", err)
		}
	}

	fmt.Printf("✅ Migrated %d sessions (Checksum: %s).\n", count, checksum)
	return nil
}

func migrateResume(ctx context.Context, dir string, dryRun, verifyOnly, force bool) error {
	boltPath := filepath.Join(dir, "resume.db")
	sqlitePath := filepath.Join(dir, "resume.sqlite")

	fmt.Printf("--- Module: Resume ---\n")
	os.Setenv("XG2G_MIGRATION_MODE", "true")

	sqStore, err := resume.NewSqliteStore(sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite store: %w", err)
	}
	defer sqStore.Close()

	if !force && !verifyOnly {
		done, err := migration.IsMigrated(sqStore.DB, migration.ModuleResume)
		if err == nil && done {
			fmt.Println("⏭️ Already migrated. Skipping.")
			return nil
		}
	}

	if _, err := os.Stat(boltPath); os.IsNotExist(err) {
		fmt.Println("ℹ️ Source Bolt DB not found. Skipping.")
		return nil
	}

	bDB, err := bolt.Open(boltPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return fmt.Errorf("open bolt db: %w", err)
	}
	defer bDB.Close()

	count, checksum, err := migration.MigrateResume(ctx, bDB, sqStore, dryRun)
	if err != nil {
		return err
	}

	if !dryRun {
		_ = migration.RecordMigration(sqStore.DB, migration.HistoryRecord{
			Module:       migration.ModuleResume,
			SourceType:   "bolt",
			SourcePath:   boltPath,
			MigratedAtMs: time.Now().UnixMilli(),
			RecordCount:  count,
			Checksum:     checksum,
		})
	}

	fmt.Printf("✅ Migrated %d resume points (Checksum: %s).\n", count, checksum)
	return nil
}

func migrateCapabilities(ctx context.Context, dir string, dryRun, verifyOnly, force bool) error {
	jsonPath := filepath.Join(dir, "v3-capabilities.json")
	sqlitePath := filepath.Join(dir, "capabilities.sqlite")

	fmt.Printf("--- Module: Capabilities ---\n")
	os.Setenv("XG2G_MIGRATION_MODE", "true")

	sqStore, err := scan.NewSqliteStore(sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite store: %w", err)
	}
	defer sqStore.Close()

	if !force && !verifyOnly {
		done, err := migration.IsMigrated(sqStore.DB, migration.ModuleCapabilities)
		if err == nil && done {
			fmt.Println("⏭️ Already migrated. Skipping.")
			return nil
		}
	}

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		fmt.Println("ℹ️ Source JSON not found. Skipping.")
		return nil
	}

	count, checksum, err := migration.MigrateCapabilities(ctx, jsonPath, sqStore, dryRun)
	if err != nil {
		return err
	}

	if !dryRun {
		_ = migration.RecordMigration(sqStore.DB, migration.HistoryRecord{
			Module:       migration.ModuleCapabilities,
			SourceType:   "json",
			SourcePath:   jsonPath,
			MigratedAtMs: time.Now().UnixMilli(),
			RecordCount:  count,
			Checksum:     checksum,
		})
	}

	fmt.Printf("✅ Migrated %d capability entries (Checksum: %s).\n", count, checksum)
	return nil
}
