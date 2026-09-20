// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/remotecore"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
)

type darwinTestIdentity struct {
	pid int
}

func (d *darwinTestIdentity) SignalGroup(sig syscall.Signal) error {
	return syscall.Kill(-d.pid, sig)
}

func (d *darwinTestIdentity) GroupExists() (bool, error) {
	err := syscall.Kill(-d.pid, 0)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, err
}

func (d *darwinTestIdentity) Close() error {
	return nil
}

func requireRealMediaCore(t *testing.T) string {
	t.Helper()
	bin := config.ResolveMediaCoreBin()
	if bin == "" {
		t.Skip("xg2g-media-core not resolved; set XG2G_MEDIA_CORE_BIN or install to /usr/local/bin")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("media core binary not usable: %v", err)
	}
	if os.Getenv("XG2G_TEST_ALLOW_DARWIN_CORE") == "1" {
		remotecore.SetProcessIdentityFactoryForTest(t.Cleanup, func(pid int) (remotecore.ProcessIdentity, error) {
			return &darwinTestIdentity{pid: pid}, nil
		})
	}
	return bin
}

func TestSessionPipeline_ProductionCutover_WithRealCore(t *testing.T) {
	_ = requireRealMediaCore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	normCfg := normalizer.DefaultConfig()
	normCfg.StartupReservoirMs = 0.0

	// Create pipeline through canonical constructor which invokes config.ResolveMediaCoreBin()
	pipe, err := NewSessionPipeline(ctx, normCfg, 20000*ring.TSPacketSize, 0)
	if err != nil {
		t.Fatalf("NewSessionPipeline failed: %v", err)
	}
	defer pipe.Close()

	if pipe.coreCloser == nil {
		t.Fatalf("SessionPipeline should have non-nil coreCloser when media core is resolved")
	}

	// Load real broadcast capture fixture and pump it through pipeline
	data := tsfixture.Load(t, "verify_final_v3.ts")
	pr, pw := io.Pipe()
	pipe.Start(ctx, pr)

	go func() {
		defer pw.Close()
		_, _ = io.Copy(pw, bytes.NewReader(data))
	}()

	// Wait for primed attach point to become available
	attach, reader, err := pipe.PrimedAttachWithTimeout(ctx, 5*time.Second)
	if err != nil {
		t.Fatalf("PrimedAttachWithTimeout failed: %v", err)
	}
	_ = reader.Close()

	if !attach.HasKeyframe {
		t.Fatalf("expected HasKeyframe to be true")
	}
	vPID, _ := pipe.MasterRing().VideoDetails()
	if vPID == 0 {
		t.Fatalf("expected non-zero VideoPID from real media core facts, got 0")
	}

	// Terminate pipeline and ensure child process is closed
	pipe.Close()
	select {
	case <-pipe.Done():
	case <-time.After(3 * time.Second):
		t.Fatalf("pipeline did not terminate within budget")
	}
}

func TestSessionPipeline_FailClosed_NoSilentFallback(t *testing.T) {
	_ = requireRealMediaCore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	normCfg := normalizer.DefaultConfig()
	normCfg.StartupReservoirMs = 0.0

	pipe, err := NewSessionPipeline(ctx, normCfg, 20000*ring.TSPacketSize, 0)
	if err != nil {
		t.Fatalf("NewSessionPipeline: %v", err)
	}
	defer pipe.Close()

	// Deliberately close the underlying remote core prematurely to simulate crash/disconnect
	if pipe.coreCloser != nil {
		_ = pipe.coreCloser.Close()
	}

	// Pushing bytes into the ring now must fail-closed rather than silently switching to Go
	packet := make([]byte, ring.TSPacketSize)
	packet[0] = ring.SyncByte
	_, pushErr := pipe.MasterRing().Push(ctx, packet)
	if pushErr == nil {
		t.Fatalf("Push after remote core close must return error, got nil")
	}
	if !errors.Is(pushErr, ring.ErrCoreUnusable) && !errors.Is(pushErr, ring.ErrCoreIncompleteResult) {
		// As long as it fails closed and does NOT succeed or fall back, contract is met
		t.Logf("Push correctly failed with: %v", pushErr)
	}
}

func TestSessionPipeline_StartupFailsClosed_WhenBinaryNotFound(t *testing.T) {
	// Isolate PATH and unset XG2G_MEDIA_CORE_BIN to guarantee binary is not resolved
	t.Setenv("XG2G_MEDIA_CORE_BIN", "")
	t.Setenv("PATH", t.TempDir())

	if bin := config.ResolveMediaCoreBin(); bin != "" {
		t.Skipf("binary resolved unexpectedly to %s; cannot test missing binary failure on this host", bin)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	normCfg := normalizer.DefaultConfig()
	pipe, err := NewSessionPipeline(ctx, normCfg, 20000*ring.TSPacketSize, 0)
	if !errors.Is(err, ErrMediaCoreNotFound) {
		t.Fatalf("expected ErrMediaCoreNotFound on unresolved media core, got: %v (pipe=%v)", err, pipe)
	}
	if pipe != nil {
		pipe.Close()
		t.Fatalf("expected nil pipeline on ErrMediaCoreNotFound, got non-nil")
	}
}
