// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// A pipe whose write side the test controls, so the upstream can be ended exactly
// when the test wants it ended.
func startPipeline(t *testing.T) (*SessionPipeline, *io.PipeWriter) {
	t.Helper()
	normCfg := normalizer.DefaultConfig()
	normCfg.StartupReservoirMs = 0.0

	pipe, err := NewSessionPipeline(normCfg, 20000*ring.TSPacketSize, 0)
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	t.Cleanup(pipe.Close)

	pr, pw := io.Pipe()
	pipe.Start(context.Background(), pr)
	return pipe, pw
}

func realCapture(t *testing.T) []byte {
	t.Helper()
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts"))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return data
}

// A broadcast that is genuinely presentable satisfies every transport criterion, and
// the await returns as soon as it does rather than at its deadline.
func TestAwait_RealBroadcastBecomesTransportReady(t *testing.T) {
	pipe, pw := startPipeline(t)
	data := realCapture(t)

	go func() {
		_, _ = pw.Write(data)
	}()

	start := time.Now()
	snap, err := AwaitTransportReady(context.Background(), pipe, quietLogger(), 10*time.Second)
	if err != nil {
		t.Fatalf("expected transport readiness, got %v", err)
	}
	if !snap.Ready {
		t.Fatalf("snapshot must be ready, pending: %v", snap.Pending)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("await returned only near the deadline (%v); it must return when the stream is ready", elapsed)
	}
	for _, c := range AllReadinessCriteria {
		if _, ok := snap.Met[c]; !ok {
			t.Errorf("criterion %s not recorded as met", c)
		}
	}
	_ = pw.Close()
}

// The measured failure this architecture exists for: the receiver answers 200 and
// then simply stops. A clean end is not success, and the outcome has to say so
// rather than leaving the caller to infer it from a nil error.
func TestAwait_CleanEOFBeforeReadinessIsNotSuccess(t *testing.T) {
	pipe, pw := startPipeline(t)

	// Enough for a PAT but nothing that could ever be presentable.
	data := realCapture(t)
	go func() {
		_, _ = pw.Write(data[:5*ring.TSPacketSize])
		_ = pw.Close() // clean EOF, no error
	}()

	_, err := AwaitTransportReady(context.Background(), pipe, quietLogger(), 5*time.Second)
	if err == nil {
		t.Fatal("a stream that ended before becoming presentable must not report success")
	}

	var notReady *TransportNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("expected TransportNotReadyError, got %T: %v", err, err)
	}
	if notReady.Outcome != OutcomeIngestEnded {
		t.Fatalf("expected outcome %q, got %q", OutcomeIngestEnded, notReady.Outcome)
	}
	if len(notReady.Snapshot.PendingCriteria()) == 0 {
		t.Fatal("the failure must name which criteria were outstanding")
	}
	// The message is what reaches an operator, so it has to carry the cause.
	if msg := notReady.Error(); msg == "" || !containsAll(msg, "ingest_ended", "outstanding") {
		t.Fatalf("failure message does not describe the failure: %q", msg)
	}
}

// A stream that never arrives at all must fail on the deadline, naming what never
// happened - not hang, and not report readiness it never observed.
func TestAwait_SilentUpstreamTimesOutWithNamedCriteria(t *testing.T) {
	pipe, pw := startPipeline(t)
	defer func() { _ = pw.Close() }()

	_, err := AwaitTransportReady(context.Background(), pipe, quietLogger(), 150*time.Millisecond)

	var notReady *TransportNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("expected TransportNotReadyError, got %T: %v", err, err)
	}
	if notReady.Outcome != OutcomeTimeout {
		t.Fatalf("expected outcome %q, got %q", OutcomeTimeout, notReady.Outcome)
	}
	pending := notReady.Snapshot.PendingCriteria()
	if len(pending) != len(AllReadinessCriteria) {
		t.Fatalf("a silent upstream leaves every criterion outstanding, got %d: %v", len(pending), pending)
	}
	// Evaluation order is the order the conditions are expected in, so the report
	// reads as a sequence rather than a set.
	if pending[0] != AllReadinessCriteria[0] {
		t.Fatalf("pending criteria must keep evaluation order, got %v", pending)
	}
}

// One preparation per client means a newer zap cancels the one in flight. The
// cancelled await must say it was abandoned, not that the channel failed.
func TestAwait_CancellationIsReportedAsAbandonedNotAsFailure(t *testing.T) {
	pipe, pw := startPipeline(t)
	defer func() { _ = pw.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	_, err := AwaitTransportReady(ctx, pipe, quietLogger(), 10*time.Second)

	var notReady *TransportNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("expected TransportNotReadyError, got %T: %v", err, err)
	}
	if notReady.Outcome != OutcomeCancelled {
		t.Fatalf("expected outcome %q, got %q", OutcomeCancelled, notReady.Outcome)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("the cancellation cause must remain inspectable through Unwrap")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
