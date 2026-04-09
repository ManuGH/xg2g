package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestPerformStartupChecksFailsWhenStorePathIsNotDirectory(t *testing.T) {
	dataDir := t.TempDir()
	storeFile := filepath.Join(dataDir, "store.sqlite")
	if err := os.WriteFile(storeFile, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("write store file: %v", err)
	}

	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storeFile,
		},
		Engine: config.EngineConfig{
			Enabled: true,
			Mode:    "virtual",
		},
		HLS: config.HLSConfig{
			Root: filepath.Join(dataDir, "hls"),
		},
	}

	if err := PerformStartupChecks(context.Background(), cfg); err == nil {
		t.Fatal("expected startup checks to fail for non-directory store path")
	}
}

func TestPerformStartupChecksFailsWhenRecordingRootIsNotWritableDirectory(t *testing.T) {
	dataDir := t.TempDir()
	recordingRoot := filepath.Join(dataDir, "recordings.json")
	if err := os.WriteFile(recordingRoot, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("write recording root file: %v", err)
	}

	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(dataDir, "store"),
		},
		Engine: config.EngineConfig{
			Enabled: false,
			Mode:    "virtual",
		},
		RecordingRoots: map[string]string{
			"main": recordingRoot,
		},
	}

	if err := PerformStartupChecks(context.Background(), cfg); err == nil {
		t.Fatal("expected startup checks to fail for non-directory recording root")
	}
}
