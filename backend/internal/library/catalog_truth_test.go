package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScannerScanRoot_PrunesRemovedItemsAndSyncsRootCount(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	writeStableLibraryFile(t, rootPath, "keep.ts")
	writeStableLibraryFile(t, rootPath, "drop.ts")

	dbPath := filepath.Join(t.TempDir(), "library.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertRoot(ctx, "movies", "local"); err != nil {
		t.Fatalf("upsert root: %v", err)
	}

	scanner := NewScanner(store)
	cfg := RootConfig{ID: "movies", Path: rootPath, Type: "local", IncludeExt: []string{".ts"}}

	result, err := scanner.ScanRoot(ctx, cfg)
	if err != nil {
		t.Fatalf("initial scan: %v", err)
	}
	if result.FinalStatus != RootStatusOK {
		t.Fatalf("expected initial scan ok, got %s", result.FinalStatus)
	}

	root, err := store.GetRoot(ctx, "movies")
	if err != nil {
		t.Fatalf("get root after initial scan: %v", err)
	}
	if root == nil || root.TotalItems != 2 {
		t.Fatalf("expected root total_items=2, got %#v", root)
	}

	if err := os.Remove(filepath.Join(rootPath, "drop.ts")); err != nil {
		t.Fatalf("remove dropped file: %v", err)
	}

	result, err = scanner.ScanRoot(ctx, cfg)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if result.FinalStatus != RootStatusOK {
		t.Fatalf("expected rescan ok, got %s", result.FinalStatus)
	}

	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	root, err = reopened.GetRoot(ctx, "movies")
	if err != nil {
		t.Fatalf("get root after reopen: %v", err)
	}
	if root == nil || root.TotalItems != 1 || root.LastScanStatus != RootStatusOK {
		t.Fatalf("expected root total_items=1 and status ok, got %#v", root)
	}

	items, total, err := reopened.GetItems(ctx, "movies", 10, 0)
	if err != nil {
		t.Fatalf("get items after reopen: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].RelPath != "keep.ts" {
		t.Fatalf("expected only keep.ts after prune, got total=%d items=%#v", total, items)
	}

	missing, err := reopened.GetItem(ctx, "movies", "drop.ts")
	if err != nil {
		t.Fatalf("get pruned item: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected pruned item to be gone, got %#v", missing)
	}
}

func TestStoreDurationPersistence_DoesNotCreateCatalogRowsAndIsPrunedBySnapshot(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	writeStableLibraryFile(t, rootPath, "film.ts")

	store, err := NewStore(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertRoot(ctx, "movies", "local"); err != nil {
		t.Fatalf("upsert root: %v", err)
	}

	if err := store.UpdateItemDuration(ctx, "movies", "film.ts", 123); err != nil {
		t.Fatalf("persist duration for scanned candidate: %v", err)
	}
	if err := store.UpdateItemDuration(ctx, "movies", "ghost.ts", 222); err != nil {
		t.Fatalf("persist duration for ghost candidate: %v", err)
	}

	item, err := store.GetItem(ctx, "movies", "film.ts")
	if err != nil {
		t.Fatalf("get film item before scan: %v", err)
	}
	if item != nil {
		t.Fatalf("expected no catalog row before scan, got %#v", item)
	}

	duration, ok, err := store.GetItemDuration(ctx, "movies", "film.ts")
	if err != nil {
		t.Fatalf("get film duration before scan: %v", err)
	}
	if !ok || duration != 123 {
		t.Fatalf("expected auxiliary duration 123 before scan, got ok=%v duration=%d", ok, duration)
	}

	scanner := NewScanner(store)
	cfg := RootConfig{ID: "movies", Path: rootPath, Type: "local", IncludeExt: []string{".ts"}}
	result, err := scanner.ScanRoot(ctx, cfg)
	if err != nil {
		t.Fatalf("scan root: %v", err)
	}
	if result.FinalStatus != RootStatusOK {
		t.Fatalf("expected scan ok, got %s", result.FinalStatus)
	}

	item, err = store.GetItem(ctx, "movies", "film.ts")
	if err != nil {
		t.Fatalf("get film item after scan: %v", err)
	}
	if item == nil || item.DurationSeconds != 123 {
		t.Fatalf("expected scanned film with persisted duration, got %#v", item)
	}

	_, ok, err = store.GetItemDuration(ctx, "movies", "ghost.ts")
	if err != nil {
		t.Fatalf("get ghost duration after scan: %v", err)
	}
	if ok {
		t.Fatal("expected ghost duration to be pruned by authoritative snapshot")
	}

	if err := os.Remove(filepath.Join(rootPath, "film.ts")); err != nil {
		t.Fatalf("remove film: %v", err)
	}
	result, err = scanner.ScanRoot(ctx, cfg)
	if err != nil {
		t.Fatalf("rescan after remove: %v", err)
	}
	if result.FinalStatus != RootStatusOK {
		t.Fatalf("expected rescan ok, got %s", result.FinalStatus)
	}

	item, err = store.GetItem(ctx, "movies", "film.ts")
	if err != nil {
		t.Fatalf("get film item after prune: %v", err)
	}
	if item != nil {
		t.Fatalf("expected film item to be pruned, got %#v", item)
	}
	_, ok, err = store.GetItemDuration(ctx, "movies", "film.ts")
	if err != nil {
		t.Fatalf("get film duration after prune: %v", err)
	}
	if ok {
		t.Fatal("expected film duration to be pruned with catalog row")
	}
}

func TestScannerScanRoot_DegradedPreservesPriorCatalog(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "outside.ts")

	writeStableLibraryFile(t, rootPath, "keep.ts")
	writeStableLibraryFile(t, rootPath, "stale.ts")
	writeStableLibraryFile(t, filepath.Dir(outsidePath), filepath.Base(outsidePath))

	store, err := NewStore(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertRoot(ctx, "movies", "local"); err != nil {
		t.Fatalf("upsert root: %v", err)
	}

	scanner := NewScanner(store)
	cfg := RootConfig{ID: "movies", Path: rootPath, Type: "local", IncludeExt: []string{".ts"}}

	if _, err := scanner.ScanRoot(ctx, cfg); err != nil {
		t.Fatalf("initial scan: %v", err)
	}

	if err := os.Remove(filepath.Join(rootPath, "stale.ts")); err != nil {
		t.Fatalf("remove stale.ts: %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(rootPath, "escape.ts")); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}

	result, err := scanner.ScanRoot(ctx, cfg)
	if err != nil {
		t.Fatalf("degraded rescan returned error: %v", err)
	}
	if result.FinalStatus != RootStatusDegraded {
		t.Fatalf("expected degraded rescan, got %s", result.FinalStatus)
	}

	root, err := store.GetRoot(ctx, "movies")
	if err != nil {
		t.Fatalf("get root after degraded scan: %v", err)
	}
	if root == nil || root.TotalItems != 2 || root.LastScanStatus != RootStatusDegraded {
		t.Fatalf("expected degraded scan to preserve prior catalog, got %#v", root)
	}

	items, total, err := store.GetItems(ctx, "movies", 10, 0)
	if err != nil {
		t.Fatalf("get items after degraded scan: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected prior catalog to survive degraded scan, got total=%d items=%#v", total, items)
	}
}

func TestStoreMarkRootScanRunning_DoesNotSetLastScanTimeWithoutCompletedScan(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertRoot(ctx, "movies", "local"); err != nil {
		t.Fatalf("upsert root: %v", err)
	}
	if err := store.MarkRootScanRunning(ctx, "movies"); err != nil {
		t.Fatalf("mark root running: %v", err)
	}

	root, err := store.GetRoot(ctx, "movies")
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if root == nil {
		t.Fatal("expected root")
	}
	if root.LastScanStatus != RootStatusRunning {
		t.Fatalf("expected running status, got %#v", root)
	}
	if root.LastScanTime != nil {
		t.Fatalf("expected no completed scan time yet, got %v", root.LastScanTime)
	}
}

func TestStoreMarkRootScanRunning_PreservesLastCompletedScanTime(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertRoot(ctx, "movies", "local"); err != nil {
		t.Fatalf("upsert root: %v", err)
	}

	completedAt := time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Second)
	if err := store.UpdateRootScanStatus(ctx, "movies", RootStatusOK, completedAt, 2); err != nil {
		t.Fatalf("set completed scan state: %v", err)
	}
	if err := store.MarkRootScanRunning(ctx, "movies"); err != nil {
		t.Fatalf("mark root running: %v", err)
	}

	root, err := store.GetRoot(ctx, "movies")
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if root == nil {
		t.Fatal("expected root")
	}
	if root.LastScanStatus != RootStatusRunning {
		t.Fatalf("expected running status, got %#v", root)
	}
	if root.LastScanTime == nil || !root.LastScanTime.Equal(completedAt) {
		t.Fatalf("expected last completed scan time %v, got %#v", completedAt, root.LastScanTime)
	}
}

func writeStableLibraryFile(t *testing.T, rootPath, relPath string) {
	t.Helper()

	absPath := filepath.Join(rootPath, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(absPath), err)
	}

	f, err := os.Create(absPath)
	if err != nil {
		t.Fatalf("create %s: %v", absPath, err)
	}
	defer func() { _ = f.Close() }()

	if err := f.Truncate(2 * 1024 * 1024); err != nil {
		t.Fatalf("truncate %s: %v", absPath, err)
	}

	oldTime := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(absPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes %s: %v", absPath, err)
	}
}
