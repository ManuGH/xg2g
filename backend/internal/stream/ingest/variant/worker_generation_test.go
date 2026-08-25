// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ingeststats"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
)

// runGenerationCut feeds a worker the first capture, waits for it to be producing,
// then feeds the second one with a bumped PMT version, and reports how the worker
// ended.
func runGenerationCut(t *testing.T, firstCapture, secondCapture string, key AudioVariantKey) error {
	t.Helper()
	skipIfNoFFmpeg(t)

	first := tsfixture.Load(t, firstCapture)
	second := tsfixture.BumpPMTVersion(t, tsfixture.Load(t, secondCapture), 1)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()

	push := func(data []byte) {
		for i := 0; i < len(data); i += 64 * ring.TSPacketSize {
			end := i + 64*ring.TSPacketSize
			if end > len(data) {
				end = len(data)
			}
			if _, err := masterRing.Push(data[i:end]); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}

	// Generation N has to be decodable before the worker attaches, otherwise the
	// primed attach has nothing to attach to.
	push(first[:len(first)/2])
	if _, ok := masterRing.LatestKeyframeOffset(); !ok {
		t.Skip("skipping: first capture produced no random access point")
	}
	generationN := masterRing.Generation()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	worker := NewAudioVariantWorker(key, masterRing, 8*1024*1024)
	worker.Start(ctx)
	defer worker.Stop()

	// The rest of generation N has to keep flowing while the worker settles:
	// FFmpeg needs input to produce output, and the primed attach below waits on
	// that output.
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		push(first[len(first)/2:])
	}()

	// Let the worker settle on generation N before anything changes.
	_, reader, err := worker.PrimedAttachWithTimeout(ctx, 15*time.Second)
	if err != nil {
		t.Skipf("skipping: worker never became primed on the first capture: %v", err)
	}
	buf := make([]byte, 32*1024)
	if _, rerr := reader.Read(buf); rerr != nil {
		t.Fatalf("worker produced no output on generation %d: %v", generationN, rerr)
	}
	_ = reader.Close()

	<-firstDone
	push(second)

	select {
	case <-worker.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("worker did not end after the upstream topology changed")
	}

	if got := masterRing.Generation(); got == generationN {
		t.Fatalf("ring generation never advanced past %d; the PMT bump was not seen", generationN)
	}
	return worker.Err()
}

// The hard case: the video codec changes under an unchanged PID while video is
// being copied. A running FFmpeg cannot follow that, so the worker must end rather
// than keep feeding HEVC into a process that probed H.264.
func TestAudioVariantWorker_VideoCodecChange_EndsWithGenerationCut(t *testing.T) {
	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		Language:          "deu",
		StreamFingerprint: "generation-cut-video",
	}

	err := runGenerationCut(t, "verify_final_v3.ts", "test_hevc_stream.ts", key)
	if !errors.Is(err, ErrUpstreamGenerationChanged) {
		t.Fatalf("worker ended with %v, want ErrUpstreamGenerationChanged", err)
	}
}

// The milder case: video stays H.264 and only the audio elementary stream changes.
// The generation is program identity, not video identity, so this must cut too -
// FFmpeg keeps its AAC decoder and would be handed MP2 payload otherwise.
func TestAudioVariantWorker_AudioCodecChange_EndsWithGenerationCut(t *testing.T) {
	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		Language:          "deu",
		StreamFingerprint: "generation-cut-audio",
	}

	err := runGenerationCut(t, "verify_final_v3.ts", "verify_seg.ts", key)
	if !errors.Is(err, ErrUpstreamGenerationChanged) {
		t.Fatalf("worker ended with %v, want ErrUpstreamGenerationChanged", err)
	}
}

// The whole cut, from a client's point of view: attached to worker N, the topology
// changes, the client's read ends cleanly rather than degrading, and the next
// attach lands on worker N+1 primed on the new generation.
//
// This is what makes ending the worker a correct policy rather than just a safe
// one. A cut the consumer cannot recover from would only move the breakage.
func TestAudioVariant_GenerationCut_ClientReattachesToNewWorker(t *testing.T) {
	skipIfNoFFmpeg(t)

	first := tsfixture.Load(t, "verify_final_v3.ts")
	second := tsfixture.BumpPMTVersion(t, tsfixture.Load(t, "test_hevc_stream.ts"), 1)

	masterRing := ring.NewMasterRing(16 * 1024 * 1024)
	defer masterRing.Close()

	push := func(data []byte) {
		for i := 0; i < len(data); i += 64 * ring.TSPacketSize {
			end := i + 64*ring.TSPacketSize
			if end > len(data) {
				end = len(data)
			}
			if _, err := masterRing.Push(data[i:end]); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}

	push(first[:len(first)/2])
	if _, ok := masterRing.LatestKeyframeOffset(); !ok {
		t.Skip("skipping: first capture produced no random access point")
	}

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	key := AudioVariantKey{
		ProgramNumber:     1,
		SourceVideoCodec:  "h264",
		TargetVideoCodec:  "copy",
		AudioPID:          257,
		TargetAudioCodec:  "aac",
		Language:          "deu",
		StreamFingerprint: "generation-cut-reattach",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	workerN, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("initial GetOrCreateWorker: %v", err)
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		push(first[len(first)/2:])
	}()

	masterGenerationN := masterRing.Generation()

	_, readerN, err := workerN.PrimedAttachWithTimeout(ctx, 15*time.Second)
	if err != nil {
		t.Skipf("skipping: never became primed on the first capture: %v", err)
	}

	buf := make([]byte, 32*1024)
	if _, rerr := readerN.Read(buf); rerr != nil {
		t.Fatalf("no output on the first generation: %v", rerr)
	}

	<-firstDone

	// Generation N+1 keeps arriving for the rest of the test. A live stream does not
	// stop while a client reconnects, and the reattach below needs input to produce
	// the output it waits on. The capture repeats at the same PMT version, so it
	// stays one generation.
	secondCtx, stopSecond := context.WithCancel(ctx)
	defer stopSecond()
	go func() {
		for secondCtx.Err() == nil {
			push(second)
		}
	}()

	// The client's read must end, and end cleanly. A reader that kept returning
	// bytes here would be delivering the new topology through a process configured
	// for the old one - exactly what the cut exists to prevent.
	deadline := time.After(25 * time.Second)
	var readErr error
	for readErr == nil {
		select {
		case <-deadline:
			t.Fatal("client read never ended after the topology changed")
		default:
		}
		_, readErr = readerN.Read(buf)
	}
	if !errors.Is(readErr, io.EOF) {
		t.Fatalf("client read ended with %v, want a clean io.EOF", readErr)
	}
	_ = readerN.Close()
	mgr.ReleaseWorkerInstance(workerN)

	if err := workerN.Err(); !errors.Is(err, ErrUpstreamGenerationChanged) {
		t.Fatalf("worker N ended with %v, want ErrUpstreamGenerationChanged", err)
	}

	// The next attach must reach a different worker, primed on the new generation.
	workerNext, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("reattach GetOrCreateWorker: %v", err)
	}
	defer mgr.ReleaseWorkerInstance(workerNext)

	if workerNext == workerN {
		t.Fatal("reattach returned the worker that was cut")
	}

	attachNext, readerNext, err := workerNext.PrimedAttachWithTimeout(ctx, 20*time.Second)
	if err != nil {
		t.Fatalf("reattach never became primed on the new generation: %v", err)
	}
	defer func() { _ = readerNext.Close() }()

	if len(attachNext.Preamble) == 0 {
		t.Fatal("reattach carried no PAT/PMT preamble")
	}
	// Deliberately not comparing attachNext.Generation with the first attach's.
	// That number is the VariantRing's own epoch, and each worker builds a fresh
	// VariantRing, so two of them count from zero along the same path and would
	// match by construction. What matters is the upstream generation the new worker
	// is serving.
	if masterRing.Generation() == masterGenerationN {
		t.Fatalf("upstream is still at generation %d; the cut was never real", masterGenerationN)
	}
	if n, rerr := readerNext.Read(buf); rerr != nil || n == 0 {
		t.Fatalf("no output on the new generation: n=%d err=%v", n, rerr)
	}
	if workerNext.Terminated() {
		t.Fatalf("replacement was cut immediately: %v", workerNext.Err())
	}
}

// Releasing must credit the worker the caller attached to, not whichever one holds
// the key now. After a cut those differ, and releasing by key alone would push the
// live replacement toward idle on behalf of subscribers that never used it.
func TestAudioVariantManager_ReleaseCreditsTheAttachedInstance(t *testing.T) {
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
		StreamFingerprint: "release-credit",
	}

	ctx := context.Background()
	old, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("first GetOrCreateWorker: %v", err)
	}

	select {
	case <-old.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not end without a decodable upstream")
	}

	replacement, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("replacement GetOrCreateWorker: %v", err)
	}
	if got := replacement.SubscriberCount(); got != 1 {
		t.Fatalf("replacement starts with %d subscribers, want 1", got)
	}

	// The old worker's client disconnects only now.
	mgr.ReleaseWorkerInstance(old)

	if got := replacement.SubscriberCount(); got != 1 {
		t.Fatalf("releasing the cut worker changed the replacement's count to %d", got)
	}
	if got := replacement.IdleDuration(); got != 0 {
		t.Fatalf("replacement was marked idle after %s while still subscribed", got)
	}
}

// A terminated worker is never handed back out. The manager replaces it, so the
// next caller gets one attached to the current topology instead of a closed ring.
func TestAudioVariantManager_ReplacesTerminatedWorker(t *testing.T) {
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
		StreamFingerprint: "replace-terminated",
	}

	ctx := context.Background()
	first, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("first GetOrCreateWorker: %v", err)
	}

	// Nothing decodable is ever pushed, so the attach times out and the worker ends.
	select {
	case <-first.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not end without a decodable upstream")
	}
	if !first.Terminated() {
		t.Fatal("Terminated() is false for a worker whose run loop has returned")
	}

	second, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("second GetOrCreateWorker: %v", err)
	}
	defer mgr.ReleaseWorker(key)

	if second == first {
		t.Fatal("manager handed back the terminated worker instead of building a new one")
	}
	if mgr.ActiveWorkerCount() != 1 {
		t.Fatalf("manager holds %d workers, want exactly the replacement", mgr.ActiveWorkerCount())
	}
}

// The reason a worker ended is what the operator sees. A topology cut and a broken
// transcoder both end a worker, and only one of them is worth paging anyone about.
func TestWorkerStopReason_DoesNotCountAGenerationCutAsAFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"generation cut", ErrUpstreamGenerationChanged, ingeststats.WorkerStopGenerationChange},
		{"wrapped generation cut", fmt.Errorf("ending worker: %w", ErrUpstreamGenerationChanged), ingeststats.WorkerStopGenerationChange},
		{"clean end", nil, ingeststats.WorkerStopShutdown},
		{"cancelled", context.Canceled, ingeststats.WorkerStopShutdown},
		{"transcoder failure", errors.New("start ffmpeg: exec format error"), ingeststats.WorkerStopError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workerStopReason(tc.err); got != tc.want {
				t.Fatalf("workerStopReason(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
