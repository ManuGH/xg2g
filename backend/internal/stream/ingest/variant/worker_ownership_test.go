// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
)

// feedMasterRing keeps a real capture flowing into a ring for the life of ctx, and
// returns once the ring holds a random access point. A worker needs continuous
// input: FFmpeg produces no output without it, and a test that stopped feeding
// would prove a worker died of starvation rather than of ownership.
func feedMasterRing(t *testing.T, ctx context.Context, r *ring.MasterRing, capture []byte) {
	t.Helper()

	push := func(data []byte) {
		for i := 0; i < len(data); i += 64 * ring.TSPacketSize {
			end := i + 64*ring.TSPacketSize
			if end > len(data) {
				end = len(data)
			}
			if _, err := r.Push(data[i:end]); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}

	push(capture[:len(capture)/2])
	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Skip("skipping: capture produced no random access point")
	}

	go func() {
		push(capture[len(capture)/2:])
		for ctx.Err() == nil {
			push(capture)
		}
	}()
}

// Ownership of a shared transcode worker.
//
// Two clients that need the same variant coalesce onto one FFmpeg process. That
// sharing is the point of the manager - but it also means the process is not the
// property of whichever client happened to ask for it first.
//
// The lifecycle that must hold:
//
//	manager (shared worker lifecycle)
//	        |
//	     worker
//	    /       \
//	client A   client B
//
// and not:
//
//	client A request context
//	        |
//	     worker
//	        |
//	client B, attached by luck
//
// A single request context may end its own subscription and nothing else. The
// worker ends on refcount zero, manager shutdown, a topology generation cut, or a
// real transcoder failure - never because one viewer closed a browser tab.
func TestAudioVariantManager_SharedWorkerOutlivesItsCreator(t *testing.T) {
	skipIfNoFFmpeg(t)

	capture := tsfixture.Load(t, "verify_final_v3.ts")

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()

	feedCtx, stopFeed := context.WithCancel(context.Background())
	defer stopFeed()
	feedMasterRing(t, feedCtx, masterRing, capture)

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		Language:          "deu",
		StreamFingerprint: "shared-worker-ownership",
	}

	// Client A arrives first and therefore creates the worker. Its context is the
	// HTTP request context the handler passes down.
	requestA, disconnectA := context.WithCancel(context.Background())
	defer disconnectA()
	workerA, err := mgr.GetOrCreateWorker(requestA, key)
	if err != nil {
		t.Fatalf("client A GetOrCreateWorker: %v", err)
	}

	// Client B coalesces onto the same process.
	requestB, disconnectB := context.WithCancel(context.Background())
	defer disconnectB()
	workerB, err := mgr.GetOrCreateWorker(requestB, key)
	if err != nil {
		t.Fatalf("client B GetOrCreateWorker: %v", err)
	}
	if workerB != workerA {
		t.Fatal("the two clients did not share a worker, so this test proves nothing")
	}
	if got := workerB.SubscriberCount(); got != 2 {
		t.Fatalf("shared worker has %d subscribers, want 2", got)
	}

	// B is streaming before anything happens to A.
	_, readerB, err := workerB.PrimedAttachWithTimeout(requestB, 20*time.Second)
	if err != nil {
		t.Skipf("skipping: client B never became primed: %v", err)
	}
	defer func() { _ = readerB.Close() }()

	buf := make([]byte, 32*1024)
	if n, rerr := readerB.Read(buf); rerr != nil || n == 0 {
		t.Fatalf("client B received no output before the disconnect: n=%d err=%v", n, rerr)
	}

	// Client A's HTTP connection ends: its request context is cancelled and its
	// lease is released. Nothing about that is a statement about B.
	disconnectA()
	mgr.ReleaseWorkerInstance(workerA)

	if got := workerB.SubscriberCount(); got != 1 {
		t.Fatalf("after A left, the shared worker has %d subscribers, want 1", got)
	}

	// The worker must simply keep running. A cancelled request context reaching the
	// FFmpeg process would end it here, and B's stream with it.
	select {
	case <-workerB.Done():
		t.Fatalf("the shared worker ended when its creator disconnected: %v", workerB.Err())
	case <-time.After(3 * time.Second):
	}

	if n, rerr := readerB.Read(buf); rerr != nil || n == 0 {
		t.Fatalf("client B lost its stream when client A disconnected: n=%d err=%v", n, rerr)
	}
}

// The other half of the ownership rule. Detaching a worker from its creator's
// request context removes the only thing that used to reclaim one, because
// CleanupIdle has no caller. If nothing replaced it, every variant a session ever
// served would hold an FFmpeg process open until the whole pipeline closed.
//
// The refcount is what decides: the last subscriber to leave stops the worker.
func TestAudioVariantManager_LastReleaseStopsTheWorker(t *testing.T) {
	skipIfNoFFmpeg(t)

	capture := tsfixture.Load(t, "verify_final_v3.ts")

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()

	feedCtx, stopFeed := context.WithCancel(context.Background())
	defer stopFeed()
	feedMasterRing(t, feedCtx, masterRing, capture)

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		Language:          "deu",
		StreamFingerprint: "refcount-reclaim",
	}

	ctx := context.Background()
	first, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("first GetOrCreateWorker: %v", err)
	}
	second, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("second GetOrCreateWorker: %v", err)
	}
	if second != first {
		t.Fatal("the two clients did not share a worker, so this test proves nothing")
	}

	// Let it actually become a running transcode, so what is reclaimed below is a
	// live process rather than a worker that never started.
	if _, reader, aerr := first.PrimedAttachWithTimeout(ctx, 20*time.Second); aerr != nil {
		t.Skipf("skipping: worker never became primed: %v", aerr)
	} else {
		_ = reader.Close()
	}

	mgr.ReleaseWorkerInstance(first)
	if first.Terminated() {
		t.Fatalf("the worker was reclaimed while a subscriber was still attached: %v", first.Err())
	}
	if got := mgr.ActiveWorkerCount(); got != 1 {
		t.Fatalf("manager holds %d workers while one is still subscribed, want 1", got)
	}

	mgr.ReleaseWorkerInstance(second)
	if !first.Terminated() {
		t.Fatal("the last subscriber left and the worker kept its FFmpeg process running")
	}
	if got := mgr.ActiveWorkerCount(); got != 0 {
		t.Fatalf("manager still holds %d workers after the last release, want 0", got)
	}
}

// A caller that has already given up must not start an FFmpeg process on its way
// out. This is the only thing the caller's context still governs.
func TestAudioVariantManager_DoesNotStartAWorkerForAnAbandonedCaller(t *testing.T) {
	masterRing := ring.NewMasterRing(1024 * 1024)
	defer masterRing.Close()

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		StreamFingerprint: "abandoned-caller",
	}

	abandoned, giveUp := context.WithCancel(context.Background())
	giveUp()

	if _, err := mgr.GetOrCreateWorker(abandoned, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrCreateWorker returned %v for a cancelled caller, want context.Canceled", err)
	}
	if got := mgr.ActiveWorkerCount(); got != 0 {
		t.Fatalf("manager started %d workers for a caller that had already gone", got)
	}
}
