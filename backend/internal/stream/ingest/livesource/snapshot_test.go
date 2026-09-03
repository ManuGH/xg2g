// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package livesource

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/pipeline"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

const graceServiceRef = "1:0:19:1:3FB:1:C00000:0:0:0:"

func captureBytes(t *testing.T) []byte {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "testdata", "segments", "verify_final_v3.ts")
		if data, err := os.ReadFile(candidate); err == nil {
			return data
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate testdata/segments/verify_final_v3.ts")
		}
		dir = parent
	}
}

func newIngestManager(t *testing.T, payload []byte) *session.Manager {
	t.Helper()
	cfg := pipeline.DefaultConnectorConfig("http://127.0.0.1", 8001)
	cfg.NormConfig.StartupReservoirMs = 50.0
	cfg.NormConfig.PacerIntervalMs = 5.0
	cfg.NormConfig.InitialBitrateKbps = 20000.0
	cfg.DialFn = func(context.Context, session.SessionKey) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	manager := session.NewManager(session.DefaultManagerConfig(), pipeline.NewLivePipelineConnector(cfg))
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

// The snapshot is what a prober gets instead of a receiver URL, so it has to be a
// real transport stream taken off the ingest the players read - not an empty file
// that ffprobe would answer with a shrug.
func TestSnapshotHead_TakesATransportStreamOffTheSharedIngest(t *testing.T) {
	manager := newIngestManager(t, captureBytes(t))
	provider := NewProvider(manager, "127.0.0.1", 8001)

	if provider.IsLive(graceServiceRef) {
		t.Fatal("IsLive is true before anything acquired the service")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	path, release, err := provider.SnapshotHead(ctx, graceServiceRef, 64<<10)
	if err != nil {
		t.Fatalf("SnapshotHead failed: %v", err)
	}

	head, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot failed: %v", err)
	}
	if len(head) < ring.TSPacketSize {
		t.Fatalf("snapshot holds %d bytes, want at least one TS packet", len(head))
	}
	if head[0] != 0x47 {
		t.Fatalf("snapshot does not start on a TS sync byte, got %#x", head[0])
	}

	// The ingest stays up while the warm hold runs, which is what lets a probe and a
	// player that arrive together share the one receiver connection.
	if !provider.IsLive(graceServiceRef) {
		t.Error("IsLive is false while the ingest session is still registered")
	}

	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("release left the snapshot behind at %s (stat err: %v)", path, err)
	}
}

// An unusable reference must not look live: the callers that ask are deciding
// whether they may open a receiver connection, and "yes, live" is the answer that
// withholds one.
func TestIsLive_IsFalseForAnUnknownOrUnusableRef(t *testing.T) {
	manager := newIngestManager(t, captureBytes(t))
	provider := NewProvider(manager, "127.0.0.1", 8001)

	if provider.IsLive("1:0:19:2B5C:3EF:1:C00000:0:0:0:") {
		t.Error("IsLive is true for a service nothing acquired")
	}
	if provider.IsLive("") {
		t.Error("IsLive is true for an empty service reference")
	}
}
