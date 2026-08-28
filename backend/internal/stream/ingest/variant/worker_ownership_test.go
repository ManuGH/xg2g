// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
)

// passthroughFFmpeg puts a stand-in ffmpeg on PATH that ignores its arguments and
// copies stdin to stdout, so the variant ring receives the same indexable capture
// the master ring holds.
//
// What these tests are about is worker lifetime, not encoding. Depending on a real
// ffmpeg would make them skip in the PR gate, which deliberately does not install
// one - and a lifetime regression that only fails on a developer's machine is a
// regression that reaches main.
func passthroughFFmpeg(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nexec cat\n"), 0o700); err != nil {
		t.Fatalf("write stand-in ffmpeg: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// feedCapture replays a real capture into the ring until the test ends, and returns
// once the ring holds a random access point.
//
// A real capture rather than filler: a worker attaches at a primed random access
// point, so a stream carrying no PSI and no keyframes can never start one, and the
// test would be measuring starvation instead of ownership.
func feedCapture(t *testing.T, r *ring.MasterRing) {
	t.Helper()

	capture := tsfixture.Load(t, "verify_final_v3.ts")

	push := func(data []byte) bool {
		for i := 0; i < len(data); i += 64 * ring.TSPacketSize {
			end := i + 64*ring.TSPacketSize
			if end > len(data) {
				end = len(data)
			}
			if _, err := r.Push(context.Background(), data[i:end]); err != nil {
				return false
			}
			time.Sleep(time.Millisecond)
		}
		return true
	}

	push(capture[:len(capture)/2])
	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Skip("skipping: capture produced no random access point")
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		<-done
	})

	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if !push(capture) {
				return
			}
		}
	}()
}

// readWithin performs one blocking read bounded by timeout.
func readWithin(t *testing.T, reader *ring.SubscriberReader, timeout time.Duration) (int, error) {
	t.Helper()

	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 32*1024)
		n, err := reader.Read(buf)
		ch <- result{n: n, err: err}
	}()

	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(timeout):
		t.Fatalf("read did not return within %s", timeout)
		return 0, nil
	}
}

func ownershipTestKey(fingerprint string) AudioVariantKey {
	return AudioVariantKey{
		StreamFingerprint: fingerprint,
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		SourceAudioCodec:  "aac",
		TargetAudioCodec:  "aac",
		Language:          "deu",
		Role:              "main",
		SampleRate:        48000,
		Channels:          2,
		BitrateKbps:       192,
	}
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
// A single request context may end its own subscription and nothing else.
func TestAudioVariantManager_SharedWorkerOutlivesItsCreator(t *testing.T) {
	passthroughFFmpeg(t)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()
	feedCapture(t, masterRing)

	mgr := newAudioVariantManager(masterRing, time.Hour, time.Hour)
	defer mgr.Close()

	key := ownershipTestKey("shared-worker-ownership")

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
		t.Fatalf("client B never became primed: %v", err)
	}
	defer func() { _ = readerB.Close() }()

	if n, rerr := readWithin(t, readerB, 5*time.Second); rerr != nil || n == 0 {
		t.Fatalf("client B received no output before the disconnect: n=%d err=%v", n, rerr)
	}

	// Client A's HTTP connection ends: its request context is cancelled and its
	// lease is released. Nothing about that is a statement about B.
	disconnectA()
	mgr.ReleaseWorkerInstance(workerA)

	if got := workerB.SubscriberCount(); got != 1 {
		t.Fatalf("after A left, the shared worker has %d subscribers, want 1", got)
	}
	select {
	case <-workerB.Done():
		t.Fatalf("the shared worker ended when its creator disconnected: %v", workerB.Err())
	case <-time.After(500 * time.Millisecond):
	}

	// Not even the most aggressive sweep may take the stream away from the
	// subscriber that is still attached. The count that matters here has just been
	// decremented, which is precisely when an idle check is most tempted to fire.
	mgr.CleanupIdle(0)
	if got := mgr.ActiveWorkerCount(); got != 1 {
		t.Fatalf("a sweep removed a worker with %d subscriber(s) still attached", workerB.SubscriberCount())
	}

	// B keeps receiving, and keeps receiving.
	deadline := time.Now().Add(time.Second)
	total := 0
	for time.Now().Before(deadline) {
		n, rerr := readWithin(t, readerB, 5*time.Second)
		if rerr != nil {
			t.Fatalf("client B lost its stream when A disconnected: %v (bytes since: %d)", rerr, total)
		}
		total += n
	}
	if total == 0 {
		t.Fatal("client B received no bytes after client A disconnected")
	}
}

// The reclaim rule, from the other side. Detaching a worker from its creator's
// request context removes what had been the only thing that ever reclaimed one, so
// the idle policy has to take it over - but it must not answer the question early.
// The last subscriber leaving is not proof the worker is unwanted: a client
// switching audio track is gone for a moment and back.
func TestAudioVariantManager_LastReleaseDefersToTheIdlePolicy(t *testing.T) {
	passthroughFFmpeg(t)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()
	feedCapture(t, masterRing)

	mgr := newAudioVariantManager(masterRing, time.Hour, time.Hour)
	defer mgr.Close()

	key := ownershipTestKey("idle-handoff")
	ctx := context.Background()

	worker, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("GetOrCreateWorker: %v", err)
	}
	if _, reader, aerr := worker.PrimedAttachWithTimeout(ctx, 20*time.Second); aerr != nil {
		t.Fatalf("worker never became primed: %v", aerr)
	} else {
		_ = reader.Close()
	}

	mgr.ReleaseWorkerInstance(worker)

	select {
	case <-worker.Done():
		t.Fatalf("the worker stopped on its last release instead of deferring to the idle policy: %v", worker.Err())
	case <-time.After(500 * time.Millisecond):
	}
	if got := mgr.ActiveWorkerCount(); got != 1 {
		t.Fatalf("the idle worker was deregistered immediately: %d workers", got)
	}

	// Once the policy does run, it reclaims.
	mgr.CleanupIdle(0)
	select {
	case <-worker.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("CleanupIdle left an abandoned worker running")
	}
	if got := mgr.ActiveWorkerCount(); got != 0 {
		t.Fatalf("manager still holds %d workers after the sweep, want 0", got)
	}
}

// The idle policy runs on its own. Nothing else reaps a worker whose subscribers
// have all left, so without the sweep every variant a session ever served would
// hold an FFmpeg process open until the whole pipeline closed.
func TestAudioVariantManager_IdleSweepStopsAbandonedWorker(t *testing.T) {
	passthroughFFmpeg(t)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()
	feedCapture(t, masterRing)

	mgr := newAudioVariantManager(masterRing, 50*time.Millisecond, 10*time.Millisecond)
	defer mgr.Close()

	worker, err := mgr.GetOrCreateWorker(context.Background(), ownershipTestKey("idle-sweep"))
	if err != nil {
		t.Fatalf("GetOrCreateWorker: %v", err)
	}

	mgr.ReleaseWorkerInstance(worker)

	select {
	case <-worker.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the idle sweep never stopped the abandoned worker")
	}
	if got := mgr.ActiveWorkerCount(); got != 0 {
		t.Fatalf("the swept worker is still registered: %d workers", got)
	}
}

// The same failure the ownership rule exists to prevent, reached from the reclaim
// side: an aggressive sweep must not take a stream away from a client that is
// watching it. Only the subscriber count answers that, never the idle duration.
func TestAudioVariantManager_CleanupIdleSparesSubscribedWorkers(t *testing.T) {
	passthroughFFmpeg(t)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()
	feedCapture(t, masterRing)

	mgr := newAudioVariantManager(masterRing, time.Hour, time.Hour)
	defer mgr.Close()

	worker, err := mgr.GetOrCreateWorker(context.Background(), ownershipTestKey("cleanup-spares"))
	if err != nil {
		t.Fatalf("GetOrCreateWorker: %v", err)
	}

	mgr.CleanupIdle(0)

	if got := mgr.ActiveWorkerCount(); got != 1 {
		t.Fatalf("cleanup removed a worker with %d subscriber(s)", worker.SubscriberCount())
	}
	select {
	case <-worker.Done():
		t.Fatalf("cleanup stopped a worker a client is watching: %v", worker.Err())
	case <-time.After(300 * time.Millisecond):
	}
}

// Close stops the workers it owns, subscribed or not: the pipeline going away is
// the one event that outranks every subscriber.
func TestAudioVariantManager_CloseStopsSubscribedWorker(t *testing.T) {
	passthroughFFmpeg(t)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()
	feedCapture(t, masterRing)

	mgr := newAudioVariantManager(masterRing, time.Hour, time.Hour)

	worker, err := mgr.GetOrCreateWorker(context.Background(), ownershipTestKey("close-stops"))
	if err != nil {
		t.Fatalf("GetOrCreateWorker: %v", err)
	}

	mgr.Close()

	select {
	case <-worker.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("a worker was still running after the manager closed")
	}
	if got := mgr.ActiveWorkerCount(); got != 0 {
		t.Fatalf("manager holds %d workers after Close, want 0", got)
	}
}

// A caller that has already given up must not start an FFmpeg process on its way
// out. This is the only thing the caller's context still governs.
func TestAudioVariantManager_DoesNotStartAWorkerForAnAbandonedCaller(t *testing.T) {
	masterRing := ring.NewMasterRing(1024 * 1024)
	defer masterRing.Close()

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	abandoned, giveUp := context.WithCancel(context.Background())
	giveUp()

	if _, err := mgr.GetOrCreateWorker(abandoned, ownershipTestKey("abandoned-caller")); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrCreateWorker returned %v for a cancelled caller, want context.Canceled", err)
	}
	if got := mgr.ActiveWorkerCount(); got != 0 {
		t.Fatalf("manager started %d workers for a caller that had already gone", got)
	}
}
