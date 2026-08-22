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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
)

// A receiver stand-in. Every test drives it explicitly: which services broadcast,
// which are silent, and how many dials were made.
type fakeReceiver struct {
	mu        sync.Mutex
	dials     int32
	broadcast map[string][]byte // serviceRef -> bytes to deliver, nil means silence
	writers   map[string]*io.PipeWriter
}

func newFakeReceiver(t *testing.T) *fakeReceiver {
	t.Helper()
	return &fakeReceiver{
		broadcast: make(map[string][]byte),
		writers:   make(map[string]*io.PipeWriter),
	}
}

// serve registers what a service delivers.
//
// Lookups go through refLookupKey because the service reference that reaches the
// dialler is not byte-for-byte the one handed to Acquire — the trailing colon is
// normalised away somewhere between the two. Keying on the raw string silently
// matched nothing, which looked exactly like a receiver delivering no data.
func (f *fakeReceiver) serve(ref string, payload []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcast[refLookupKey(ref)] = payload
}

func refLookupKey(ref string) string {
	return strings.TrimSuffix(ref, ":")
}

func (f *fakeReceiver) dial(_ context.Context, key session.SessionKey) (io.ReadCloser, error) {
	atomic.AddInt32(&f.dials, 1)
	f.mu.Lock()
	payload := f.broadcast[refLookupKey(key.ServiceRef)]
	pr, pw := io.Pipe()
	f.writers[refLookupKey(key.ServiceRef)] = pw
	f.mu.Unlock()

	go func() {
		if payload == nil {
			return // on air but delivering nothing, which the receiver really does
		}
		_, _ = pw.Write(payload)
	}()
	return pr, nil
}

// endBroadcast closes a service's upstream, which is the clean-EOF case.
func (f *fakeReceiver) endBroadcast(ref string) {
	f.mu.Lock()
	w := f.writers[refLookupKey(ref)]
	f.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

func (f *fakeReceiver) dialCount() int32 { return atomic.LoadInt32(&f.dials) }

func presentableBroadcast(t *testing.T) []byte {
	t.Helper()
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts"))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return data
}

func newPrepManager(t *testing.T, recv *fakeReceiver, cfg PreparationConfig) (*PreparationManager, *session.Manager) {
	t.Helper()
	connectorCfg := DefaultConnectorConfig("", 8001)
	connectorCfg.NormConfig.StartupReservoirMs = 0.0
	connectorCfg.NormConfig.PacerIntervalMs = 5.0
	connectorCfg.DialFn = recv.dial

	mgr := session.NewManager(session.ManagerConfig{
		WarmHoldDuration: 200 * time.Millisecond,
		ConnectTimeout:   2 * time.Second,
	}, NewLivePipelineConnector(connectorCfg))
	t.Cleanup(func() { _ = mgr.Close() })

	pm := NewPreparationManager(mgr, cfg, quietLogger())
	t.Cleanup(pm.Close)
	return pm, mgr
}

// waitAttachable blocks until a pipeline can actually serve a subscriber, which is
// what "the viewer is watching this" means. Asserting about an untouched channel
// before it has produced its first entry point tests nothing.
func waitAttachable(t *testing.T, pipe *SessionPipeline, within time.Duration) {
	t.Helper()
	waitFor(t, within, func() bool {
		_, reader, err := pipe.PrimedAttach()
		if err == nil {
			reader.Close()
			return true
		}
		return false
	})
}

func keyFor(ref string) session.SessionKey {
	return session.NewSessionKey("127.0.0.1", 8001, ref)
}

func awaitTerminalOrReady(t *testing.T, p *Preparation, within time.Duration) PreparationStatus {
	t.Helper()
	deadline := time.After(within)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		st := p.Status()
		if st.State == PreparationReady || st.State.Terminal() {
			return st
		}
		select {
		case <-deadline:
			t.Fatalf("preparation never settled, last state %q", st.State)
		case <-tick.C:
		}
	}
}

const (
	refA = "1:0:19:AAAA:0:0:0:0:0:0:"
	refB = "1:0:19:BBBB:0:0:0:0:0:0:"
	refC = "1:0:19:CCCC:0:0:0:0:0:0:"
)

// Proof 1: A is playing, preparing B fails, and A is untouched.
//
// This is the property the whole transaction exists for. A failed channel change
// must cost the viewer nothing at all — not a stutter, not a reconnect, and above
// all not the frozen picture that was measured before any of this existed.
func TestPrepare_FailureLeavesTheRunningChannelUntouched(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refA, presentableBroadcast(t))
	recv.serve(refB, nil) // on air, delivering nothing

	cfg := DefaultPreparationConfig()
	cfg.ReadyTimeout = 250 * time.Millisecond
	pm, mgr := newPrepManager(t, recv, cfg)

	// A is being watched: its own lease, held by its own request.
	leaseA, err := mgr.Acquire(context.Background(), keyFor(refA))
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	defer leaseA.Release()
	pipeA := leaseA.Session().Payload().(*SessionPipeline)
	waitAttachable(t, pipeA, 5*time.Second)

	prep, err := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-1", Key: keyFor(refB)})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	st := awaitTerminalOrReady(t, prep, 3*time.Second)

	if st.State != PreparationFailed {
		t.Fatalf("expected failure for a silent service, got %q", st.State)
	}
	if st.Outcome != OutcomeTimeout {
		t.Fatalf("expected a named timeout outcome, got %q", st.Outcome)
	}
	if len(st.Pending) == 0 {
		t.Fatal("a failed preparation must name what was outstanding")
	}

	// A is still alive and still serving.
	select {
	case <-pipeA.Done():
		t.Fatal("the running channel was torn down by an unrelated preparation")
	default:
	}
	if _, _, err := pipeA.PrimedAttach(); err != nil {
		t.Fatalf("the running channel must still be attachable: %v", err)
	}
}

// Proof 2: readiness alone changes nothing. Until the client commits, the server has
// not switched anything — it has only proven that it could.
func TestPrepare_ReadyDoesNotSwitchAnythingByItself(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refA, presentableBroadcast(t))
	recv.serve(refB, presentableBroadcast(t))

	pm, mgr := newPrepManager(t, recv, DefaultPreparationConfig())

	leaseA, err := mgr.Acquire(context.Background(), keyFor(refA))
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	defer leaseA.Release()
	pipeA := leaseA.Session().Payload().(*SessionPipeline)
	waitAttachable(t, pipeA, 5*time.Second)

	prep, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-2", Key: keyFor(refB)})
	st := awaitTerminalOrReady(t, prep, 5*time.Second)

	if st.State != PreparationReady {
		t.Fatalf("expected ready, got %q (%s)", st.State, st.Detail)
	}
	if st.Generation == 0 {
		t.Fatal("a ready preparation must name the generation it proved")
	}

	// A is untouched and still the only thing being served to this client.
	select {
	case <-pipeA.Done():
		t.Fatal("preparing another channel ended the running one")
	default:
	}
	if got := prep.Status().State; got != PreparationReady {
		t.Fatalf("state must stay ready until the client commits, got %q", got)
	}
}

// Commit is generation-bound and idempotent: a retry after a lost response must not
// tell the client its channel change failed, and a commit naming a generation the
// stream has left must be refused rather than silently switching to another stream.
func TestPrepare_CommitIsGenerationBoundAndIdempotent(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	pm, _ := newPrepManager(t, recv, DefaultPreparationConfig())

	prep, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-3", Key: keyFor(refB)})
	st := awaitTerminalOrReady(t, prep, 5*time.Second)
	if st.State != PreparationReady {
		t.Fatalf("expected ready, got %q", st.State)
	}

	if _, err := pm.Commit(prep.ID(), st.Generation+99); !errors.Is(err, ErrGenerationChanged) {
		t.Fatalf("a stale generation must be refused, got %v", err)
	}

	first, err := pm.Commit(prep.ID(), st.Generation)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if first.State != PreparationCommitted {
		t.Fatalf("expected committed, got %q", first.State)
	}

	second, err := pm.Commit(prep.ID(), st.Generation)
	if err != nil {
		t.Fatalf("commit must be idempotent, second attempt failed: %v", err)
	}
	if second.State != PreparationCommitted {
		t.Fatalf("expected committed on retry, got %q", second.State)
	}
}

// Committing something that was never ready is refused. HTTP 200 is not readiness,
// and neither is "the preparation exists".
func TestPrepare_CommitBeforeReadinessIsRefused(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, nil)
	cfg := DefaultPreparationConfig()
	cfg.ReadyTimeout = 2 * time.Second
	pm, _ := newPrepManager(t, recv, cfg)

	prep, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-4", Key: keyFor(refB)})

	if _, err := pm.Commit(prep.ID(), 1); !errors.Is(err, ErrPreparationNotReady) {
		t.Fatalf("expected refusal before readiness, got %v", err)
	}
}

// Proof 4: a newer zap supersedes the one in flight, and the superseded preparation
// gives its tuner back. Zapping down a channel list must never accumulate tuners.
func TestPrepare_NewerZapSupersedesAndReleasesTheOlderOne(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, nil) // B never becomes ready, so it is still in flight
	recv.serve(refC, presentableBroadcast(t))

	cfg := DefaultPreparationConfig()
	cfg.ReadyTimeout = 5 * time.Second
	pm, mgr := newPrepManager(t, recv, cfg)

	prepB, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-b", Key: keyFor(refB)})

	// Let B actually take its lease before superseding it, so the release is real.
	waitFor(t, time.Second, func() bool { return mgr.ActiveCount() >= 1 })

	prepC, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-c", Key: keyFor(refC)})

	stB := awaitTerminalOrReady(t, prepB, 2*time.Second)
	if stB.State != PreparationCancelled {
		t.Fatalf("the superseded preparation must be cancelled, got %q", stB.State)
	}

	stC := awaitTerminalOrReady(t, prepC, 5*time.Second)
	if stC.State != PreparationReady {
		t.Fatalf("the newer preparation must proceed, got %q (%s)", stC.State, stC.Detail)
	}

	// B's ingest must be gone once the warm hold lapses; only C's remains.
	waitFor(t, 3*time.Second, func() bool { return mgr.ActiveCount() == 1 })

	if _, ok := pm.ActiveForClient("sterling-1"); !ok {
		t.Fatal("the client should still own its newest preparation")
	}
}

// Proof 6/“no leak”: a client that disappears after preparing must not pin a tuner.
// Nobody commits, and the preparation has to expire on its own.
func TestPrepare_UncommittedPreparationExpiresAndReleasesItsTuner(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))

	cfg := DefaultPreparationConfig()
	cfg.CommitTimeout = 300 * time.Millisecond
	pm, mgr := newPrepManager(t, recv, cfg)

	prep, _ := pm.Prepare(PrepareRequest{ClientID: "vanished", ZapID: "zap-5", Key: keyFor(refB)})
	if st := awaitTerminalOrReady(t, prep, 5*time.Second); st.State != PreparationReady {
		t.Fatalf("expected ready, got %q", st.State)
	}

	<-prep.Done()
	if st := prep.Status(); st.State != PreparationCancelled {
		t.Fatalf("an uncommitted preparation must expire, got %q", st.State)
	}
	waitFor(t, 3*time.Second, func() bool { return mgr.ActiveCount() == 0 })
}

// Explicit cancellation releases the tuner immediately — this is what a client
// dropping a zap in flight calls.
func TestPrepare_CancelReleasesImmediately(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	pm, mgr := newPrepManager(t, recv, DefaultPreparationConfig())

	prep, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-6", Key: keyFor(refB)})
	waitFor(t, 2*time.Second, func() bool { return mgr.ActiveCount() >= 1 })

	if !pm.Cancel(prep.ID(), "client went away") {
		t.Fatal("cancel should report that it acted")
	}
	if st := prep.Status(); st.State != PreparationCancelled {
		t.Fatalf("expected cancelled, got %q", st.State)
	}
	waitFor(t, 3*time.Second, func() bool { return mgr.ActiveCount() == 0 })

	if pm.Cancel(prep.ID(), "again") {
		t.Fatal("cancelling twice must not act twice")
	}
}

// Proof 7: the upstream ends before readiness. The failure is named — a clean EOF is
// not success — and the channel the viewer is watching is unaffected.
func TestPrepare_UpstreamEOFBeforeReadinessIsNamed(t *testing.T) {
	recv := newFakeReceiver(t)
	data := presentableBroadcast(t)
	recv.serve(refA, data)
	recv.serve(refB, data[:5*188]) // a few packets, then EOF

	cfg := DefaultPreparationConfig()
	cfg.ReadyTimeout = 5 * time.Second
	pm, mgr := newPrepManager(t, recv, cfg)

	leaseA, err := mgr.Acquire(context.Background(), keyFor(refA))
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	defer leaseA.Release()
	pipeA := leaseA.Session().Payload().(*SessionPipeline)
	waitAttachable(t, pipeA, 5*time.Second)

	prep, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "zap-7", Key: keyFor(refB)})
	waitFor(t, 2*time.Second, func() bool { return recv.dialCount() >= 2 })
	recv.endBroadcast(refB)

	st := awaitTerminalOrReady(t, prep, 5*time.Second)
	if st.State != PreparationFailed {
		t.Fatalf("expected failure, got %q", st.State)
	}
	if st.Outcome != OutcomeIngestEnded && st.Outcome != OutcomeTimeout {
		t.Fatalf("the outcome must name the cause, got %q", st.Outcome)
	}
	if st.Detail == "" {
		t.Fatal("a failure must carry a description")
	}

	if _, _, err := pipeA.PrimedAttach(); err != nil {
		t.Fatalf("the watched channel must be unaffected: %v", err)
	}
}

// Proof 9: two clients prepare at once and do not disturb each other. "One
// preparation per client" is per client, not global.
func TestPrepare_TwoClientsDoNotCancelEachOther(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	recv.serve(refC, presentableBroadcast(t))
	pm, _ := newPrepManager(t, recv, DefaultPreparationConfig())

	p1, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "z1", Key: keyFor(refB)})
	p2, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-2", ZapID: "z2", Key: keyFor(refC)})

	st1 := awaitTerminalOrReady(t, p1, 5*time.Second)
	st2 := awaitTerminalOrReady(t, p2, 5*time.Second)

	if st1.State != PreparationReady {
		t.Fatalf("client 1 preparation must survive, got %q (%s)", st1.State, st1.Detail)
	}
	if st2.State != PreparationReady {
		t.Fatalf("client 2 preparation must survive, got %q (%s)", st2.State, st2.Detail)
	}
}

// Proof 8: preparing a service somebody is already watching costs no second dial to
// the receiver. The preparation coalesces onto the running ingest and is ready
// almost at once — same semantics, far less waiting.
func TestPrepare_WarmServiceCoalescesWithoutASecondDial(t *testing.T) {
	recv := newFakeReceiver(t)
	recv.serve(refB, presentableBroadcast(t))
	pm, mgr := newPrepManager(t, recv, DefaultPreparationConfig())

	watcher, err := mgr.Acquire(context.Background(), keyFor(refB))
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer watcher.Release()
	dialsAfterWatcher := recv.dialCount()

	start := time.Now()
	prep, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-2", ZapID: "z-warm", Key: keyFor(refB)})
	st := awaitTerminalOrReady(t, prep, 5*time.Second)

	if st.State != PreparationReady {
		t.Fatalf("expected ready, got %q (%s)", st.State, st.Detail)
	}
	if got := recv.dialCount(); got != dialsAfterWatcher {
		t.Fatalf("preparing a warm service must not dial again: %d -> %d", dialsAfterWatcher, got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("a warm preparation should settle quickly, took %v", elapsed)
	}
}

// Proof 5: preparations are not exempt from admission. A preparation takes its lease
// through the same path a viewer does, so whatever refuses a viewer refuses it.
func TestPrepare_AdmissionRefusalIsTerminalAndNamed(t *testing.T) {
	connectorCfg := DefaultConnectorConfig("", 8001)
	connectorCfg.NormConfig.StartupReservoirMs = 0.0
	connectorCfg.DialFn = func(context.Context, session.SessionKey) (io.ReadCloser, error) {
		return nil, errors.New("tuner topology admission denied")
	}
	mgr := session.NewManager(session.ManagerConfig{
		WarmHoldDuration: 50 * time.Millisecond,
		ConnectTimeout:   500 * time.Millisecond,
	}, NewLivePipelineConnector(connectorCfg))
	defer func() { _ = mgr.Close() }()

	pm := NewPreparationManager(mgr, DefaultPreparationConfig(), quietLogger())
	defer pm.Close()

	prep, _ := pm.Prepare(PrepareRequest{ClientID: "sterling-1", ZapID: "z-denied", Key: keyFor(refB)})
	st := awaitTerminalOrReady(t, prep, 5*time.Second)

	if st.State != PreparationFailed {
		t.Fatalf("expected failure, got %q", st.State)
	}
	if st.Outcome != OutcomeAdmissionDenied {
		t.Fatalf("expected %q, got %q", OutcomeAdmissionDenied, st.Outcome)
	}
	if st.Detail == "" {
		t.Fatal("an admission refusal must say so")
	}
}

func waitFor(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.After(within)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("condition never became true")
		case <-tick.C:
		}
	}
}
