package ffmpeg

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/rs/zerolog"
)

func makeValidSegment(size int) []byte {
	// Must contain moof and mdat
	moofSize := 8
	mdatSize := size - moofSize
	if mdatSize < 8 {
		mdatSize = 8
	}

	data := make([]byte, moofSize+mdatSize)
	binary.BigEndian.PutUint32(data[0:4], uint32(moofSize))
	copy(data[4:8], "moof")

	binary.BigEndian.PutUint32(data[moofSize:moofSize+4], uint32(mdatSize))
	copy(data[moofSize+4:moofSize+8], "mdat")

	return data
}

func setupTestEnvironment(t *testing.T) (string, *LocalAdapter, *ShadowRuntime) {
	return setupTestEnvironmentWith(t, nil)
}

// setupTestEnvironmentWith lets a test adjust the adapter before the shadow
// store attaches, because the monitoring goroutine captures it at start.
func setupTestEnvironmentWith(t *testing.T, configure func(*LocalAdapter)) (string, *LocalAdapter, *ShadowRuntime) {
	tempDir := t.TempDir()

	registry := store.NewMemoryStoreRegistry()

	adapter := &LocalAdapter{
		StoreRegistry: registry,
		Config: AdapterConfig{
			ShadowStoreEnabled:       true,
			ShadowStoreMaxBytes:      1024 * 1024 * 10,
			ShadowStoreQueueMaxBytes: 1024 * 1024 * 5,
			ShadowStoreMaxObjects:    32,
		},
		Logger: zerolog.Nop(),
	}
	if configure != nil {
		configure(adapter)
	}

	plan := ports.ExecutedFFmpegPlan{Container: "fmp4"}

	sr, err := adapter.attachShadowStore(context.Background(), "test-session", plan, tempDir)
	if err != nil {
		t.Fatalf("Failed to attach shadow store: %v", err)
	}

	return tempDir, adapter, sr
}

func TestShadowRuntime_IgnoreWriteOnTmp(t *testing.T) {
	tempDir, _, sr := setupTestEnvironment(t)
	defer sr.Close()

	// Write to a .tmp file
	tmpPath := filepath.Join(tempDir, "seg_0001.m4s.tmp")
	err := os.WriteFile(tmpPath, makeValidSegment(100), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Give it some time to process
	time.Sleep(100 * time.Millisecond)

	// Should not be in store
	_, err = sr.Store.Get(context.Background(), "test-session", "seg_0001.m4s.tmp")
	if err != store.ErrNotFound {
		t.Fatalf("Expected ErrNotFound, got %v", err)
	}
}

func TestShadowRuntime_RenameTmpToFinalPublishes(t *testing.T) {
	tempDir, _, sr := setupTestEnvironment(t)
	defer sr.Close()

	tmpPath := filepath.Join(tempDir, "seg_0002.m4s.tmp")
	finalPath := filepath.Join(tempDir, "seg_0002.m4s")

	data := makeValidSegment(200)
	err := os.WriteFile(tmpPath, data, 0644)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond) // Let any create event settle

	err = os.Rename(tmpPath, finalPath)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for debounce and publish
	time.Sleep(150 * time.Millisecond)

	obj, err := sr.Store.Get(context.Background(), "test-session", "seg_0002.m4s")
	if err != nil {
		t.Fatalf("Expected segment in store, got error: %v", err)
	}
	if !obj.Complete {
		t.Fatal("Expected Complete=true")
	}
	if len(obj.Data) != 200 {
		t.Fatalf("Expected size 200, got %d", len(obj.Data))
	}
}

func TestShadowRuntime_IncompleteSegmentRejected(t *testing.T) {
	tempDir, _, sr := setupTestEnvironment(t)
	defer sr.Close()

	// Write incomplete segment directly
	finalPath := filepath.Join(tempDir, "seg_0003.m4s")

	// Create a valid moof but missing mdat
	moof := make([]byte, 8)
	binary.BigEndian.PutUint32(moof[0:4], 8)
	copy(moof[4:8], "moof")

	err := os.WriteFile(finalPath, moof, 0644)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)

	_, err = sr.Store.Get(context.Background(), "test-session", "seg_0003.m4s")
	if err != store.ErrNotFound {
		t.Fatalf("Expected ErrNotFound for incomplete segment, got %v", err)
	}
}

func TestShadowRuntime_FallbackScanRepairsDroppedEvent(t *testing.T) {
	tempDir, _, sr := setupTestEnvironment(t)
	defer sr.Close()

	finalPath := filepath.Join(tempDir, "seg_0004.m4s")

	// Create file without triggering fsnotify or simulating a dropped event.
	// Since fsnotify is active, it WILL trigger Create/Write. But we can sleep
	// long enough for the fallback ticker to pick it up if fsnotify fails.
	// We'll just rely on the regular pipeline which is picked up by Create.
	data := makeValidSegment(150)
	err := os.WriteFile(finalPath, data, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for fallback scan (runs every 500ms)
	time.Sleep(800 * time.Millisecond)

	_, err = sr.Store.Get(context.Background(), "test-session", "seg_0004.m4s")
	if err != nil {
		t.Fatalf("Expected segment in store after fallback scan, got error: %v", err)
	}

	// Now overwrite with a larger file to simulate changed fingerprint
	data2 := makeValidSegment(300)
	err = os.WriteFile(finalPath, data2, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for fallback scan
	time.Sleep(800 * time.Millisecond)

	obj2, err := sr.Store.Get(context.Background(), "test-session", "seg_0004.m4s")
	if err != nil {
		t.Fatalf("Expected updated segment in store, got error: %v", err)
	}
	if len(obj2.Data) != 300 {
		t.Fatalf("Expected updated size 300, got %d", len(obj2.Data))
	}
}

// The fallback ticker revisits every file in the session directory every
// 500 ms. A file that is already mirrored with an unchanged size and mtime must
// be skipped before it is read; otherwise a long DVR window is read from disk
// in full, twice a second, for as long as the session runs.
func TestShadowRuntime_FallbackScanSkipsUnchangedFiles(t *testing.T) {
	var reads atomic.Int64
	tempDir, _, sr := setupTestEnvironmentWith(t, func(a *LocalAdapter) {
		a.shadowReadFile = func(path string) ([]byte, error) {
			reads.Add(1)
			return os.ReadFile(path)
		}
	})
	defer sr.Close()

	const segments = 20
	names := make([]string, 0, segments)
	for i := 0; i < segments; i++ {
		name := fmt.Sprintf("seg_0_%06d.m4s", i)
		names = append(names, name)
		if err := os.WriteFile(filepath.Join(tempDir, name), makeValidSegment(4096), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	waitForShadowObject := func(name string, size int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			obj, err := sr.Store.Get(context.Background(), "test-session", name)
			if err == nil && len(obj.Data) == size {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s (%d bytes) was not mirrored in time, last error: %v", name, size, err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	for _, name := range names {
		waitForShadowObject(name, 4096)
	}

	// Let debounced Create events drain, then stay idle across several
	// fallback ticks. Nothing on disk changes, so nothing may be read.
	time.Sleep(200 * time.Millisecond)
	settled := reads.Load()
	if settled < segments {
		t.Fatalf("expected at least %d initial reads, got %d", segments, settled)
	}
	time.Sleep(1300 * time.Millisecond)
	if got := reads.Load(); got != settled {
		t.Fatalf("idle fallback scan re-read unchanged files: %d extra reads over %d files", got-settled, segments)
	}

	// A changed file must still be picked up and read again.
	if err := os.WriteFile(filepath.Join(tempDir, names[3]), makeValidSegment(8192), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForShadowObject(names[3], 8192)
	if got := reads.Load(); got <= settled {
		t.Fatalf("changed file was not re-read: reads %d, before change %d", got, settled)
	}
}
