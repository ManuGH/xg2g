package vod

import (
	"context"
	"os"
	"testing"
)

type stubPathMapper struct {
	path string
	ok   bool
}

func (m *stubPathMapper) ResolveLocalExisting(receiverPath string) (string, bool) {
	return m.path, m.ok
}

type successProber struct{}

func (p *successProber) Probe(ctx context.Context, path string) (*StreamInfo, error) {
	// Return valid info so probe succeeds
	return &StreamInfo{
		Video: VideoStreamInfo{Duration: 60.0},
	}, nil
}

func TestRunProbe_UsesResolvedPathFromMetadata(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "probe-meta-*.ts")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	mgr, err := NewManager(&mockRunner{}, &successProber{}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	id := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	mgr.SeedMetadata(id, Metadata{
		ResolvedPath: tmpFile.Name(),
	})

	_ = mgr.runProbe(context.Background(), probeRequest{ServiceRef: id, InputPath: ""})

	meta, ok := mgr.GetMetadata(id)
	if !ok {
		t.Fatal("metadata not found after probe")
	}
	if meta.State != ArtifactStateReady {
		t.Fatalf("expected READY, got %s", meta.State)
	}
	if meta.ResolvedPath != tmpFile.Name() {
		t.Fatalf("expected resolved path %q, got %q", tmpFile.Name(), meta.ResolvedPath)
	}
}

func TestRunProbe_UsesPathMapperWhenInputEmpty(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "probe-map-*.ts")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	pm := &stubPathMapper{path: tmpFile.Name(), ok: true}
	mgr, err := NewManager(&mockRunner{}, &successProber{}, pm)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	id := "1:0:0:0:0:0:0:0:0:/media/test.ts"

	_ = mgr.runProbe(context.Background(), probeRequest{ServiceRef: id, InputPath: ""})

	meta, ok := mgr.GetMetadata(id)
	if !ok {
		t.Fatal("metadata not found after probe")
	}
	if meta.State != ArtifactStateReady {
		t.Fatalf("expected READY, got %s", meta.State)
	}
	if meta.ResolvedPath != tmpFile.Name() {
		t.Fatalf("expected resolved path %q, got %q", tmpFile.Name(), meta.ResolvedPath)
	}
}

func TestRunProbe_EmptyInputFailsWithoutResolver(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	id := "1:0:0:0:0:0:0:0:0:/media/test.ts"

	_ = mgr.runProbe(context.Background(), probeRequest{ServiceRef: id, InputPath: ""})

	meta, ok := mgr.GetMetadata(id)
	if !ok {
		t.Fatal("metadata not found after probe")
	}
	if meta.State != ArtifactStateFailed {
		t.Fatalf("expected FAILED, got %s", meta.State)
	}
	if meta.Error == "" {
		t.Fatal("expected error on failed probe")
	}
}
