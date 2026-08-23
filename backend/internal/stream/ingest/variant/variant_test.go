// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

func TestAudioVariantKey_String(t *testing.T) {
	k1 := AudioVariantKey{
		StreamFingerprint: "fp123",
		ProgramNumber:     132,
		AudioPID:          2281,
		Language:          "deu",
		Role:              "main",
		TargetCodec:       "aac",
		SampleRate:        48000,
		Channels:          2,
		BitrateKbps:       192,
	}

	k2 := AudioVariantKey{
		StreamFingerprint: "fp123",
		ProgramNumber:     132,
		AudioPID:          2282,
		Language:          "deu",
		Role:              "audio_description",
		TargetCodec:       "aac",
		SampleRate:        48000,
		Channels:          2,
		BitrateKbps:       128,
	}

	if k1.String() == k2.String() {
		t.Fatalf("expected distinct strings for different PIDs/roles, got identical: %s", k1.String())
	}
}

func TestAudioVariantManager_SubscriberSharing(t *testing.T) {
	masterRing := ring.NewMasterRingWithProgram(1024*1024, 132)
	defer masterRing.Close()

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	key := AudioVariantKey{
		StreamFingerprint: "test-stream",
		ProgramNumber:     132,
		AudioPID:          2281,
		Language:          "deu",
		Role:              "main",
		TargetCodec:       "aac",
		SampleRate:        48000,
		Channels:          2,
		BitrateKbps:       192,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Client 1 requests variant
	w1, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("failed to create worker for client 1: %v", err)
	}

	if mgr.ActiveWorkerCount() != 1 {
		t.Fatalf("expected 1 active worker, got %d", mgr.ActiveWorkerCount())
	}
	if w1.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", w1.SubscriberCount())
	}

	// Client 2 requests SAME variant
	w2, err := mgr.GetOrCreateWorker(ctx, key)
	if err != nil {
		t.Fatalf("failed to get worker for client 2: %v", err)
	}

	if w1 != w2 {
		t.Fatalf("expected identical worker instance for shared variant key, got different pointers")
	}
	if mgr.ActiveWorkerCount() != 1 {
		t.Fatalf("expected still 1 active worker, got %d", mgr.ActiveWorkerCount())
	}
	if w1.SubscriberCount() != 2 {
		t.Fatalf("expected 2 subscribers on shared worker, got %d", w1.SubscriberCount())
	}

	// Client 1 releases
	mgr.ReleaseWorker(key)
	if w1.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber remaining, got %d", w1.SubscriberCount())
	}

	// Client 2 releases
	mgr.ReleaseWorker(key)
	if w1.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers remaining, got %d", w1.SubscriberCount())
	}

	// Cleanup idle workers with 0 duration
	mgr.CleanupIdle(0)
	if mgr.ActiveWorkerCount() != 0 {
		t.Fatalf("expected 0 active workers after cleanup, got %d", mgr.ActiveWorkerCount())
	}
}

func TestAudioVariantManager_DistinctTrackWorkers(t *testing.T) {
	masterRing := ring.NewMasterRingWithProgram(1024*1024, 132)
	defer masterRing.Close()

	mgr := NewAudioVariantManager(masterRing)
	defer mgr.Close()

	key1 := AudioVariantKey{
		StreamFingerprint: "test-stream",
		ProgramNumber:     132,
		AudioPID:          2281, // Main Deutsch
		Language:          "deu",
		Role:              "main",
		TargetCodec:       "aac",
		SampleRate:        48000,
		Channels:          2,
		BitrateKbps:       192,
	}

	key2 := AudioVariantKey{
		StreamFingerprint: "test-stream",
		ProgramNumber:     132,
		AudioPID:          2282, // Audiodeskription
		Language:          "deu",
		Role:              "audio_description",
		TargetCodec:       "aac",
		SampleRate:        48000,
		Channels:          2,
		BitrateKbps:       128,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w1, err := mgr.GetOrCreateWorker(ctx, key1)
	if err != nil {
		t.Fatalf("failed to create worker 1: %v", err)
	}
	w2, err := mgr.GetOrCreateWorker(ctx, key2)
	if err != nil {
		t.Fatalf("failed to create worker 2: %v", err)
	}

	if w1 == w2 {
		t.Fatalf("expected distinct workers for distinct audio PIDs/roles, got same worker")
	}
	if mgr.ActiveWorkerCount() != 2 {
		t.Fatalf("expected 2 active workers, got %d", mgr.ActiveWorkerCount())
	}
}

func TestAudioVariantWorker_RealCapture_TranscodeAndAttach(t *testing.T) {
	capturePath := "../../../../testdata/segments/verify_final_v3.ts"
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Skipf("skipping real capture test: file %s not found: %v", capturePath, err)
		return
	}

	masterRing := ring.NewMasterRing(len(data) + 1024*1024)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Push capture continuously until context ends
	go func() {
		defer masterRing.Close()
		chunkSize := 10 * ring.TSPacketSize
		for {
			for i := 0; i < len(data); i += chunkSize {
				if ctx.Err() != nil {
					return
				}
				end := i + chunkSize
				if end > len(data) {
					end = len(data)
				}
				if (end-i)%ring.TSPacketSize != 0 {
					end = i + ((end-i)/ring.TSPacketSize)*ring.TSPacketSize
				}
				if end > i {
					if _, perr := masterRing.Push(data[i:end]); perr != nil {
						return
					}
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	key := AudioVariantKey{
		StreamFingerprint: "capture-test",
		ProgramNumber:     1,
		AudioPID:          257,
		Language:          "deu",
		Role:              "main",
		TargetCodec:       "aac",
		SampleRate:        48000,
		Channels:          2,
		BitrateKbps:       192,
	}

	worker := NewAudioVariantWorker(key, masterRing, 4*1024*1024)
	worker.Start(ctx)
	defer worker.Stop()

	attach, reader, err := worker.PrimedAttachWithTimeout(ctx, 4*time.Second)
	if err != nil {
		facts := worker.Ring().ReadinessFacts()
		t.Fatalf("worker.PrimedAttachWithTimeout failed: %v | variant facts: hasPAT=%v hasPMT=%v vPID=%d vCodec=%v audioPIDs=%v cleanRAP=%d cleanAU=%d keyframeOffsets=%d",
			err, facts.HasPAT, facts.HasPMT, facts.VideoPID, facts.VideoCodec, facts.AudioPIDs, facts.CleanEntryPoints, facts.CleanAccessUnits, facts.CleanEntryPoints)
	}
	defer reader.Close()

	if len(attach.Preamble) == 0 {
		t.Fatalf("expected non-empty preamble on audio variant attach")
	}

	vFacts := worker.Ring().ReadinessFacts()
	if vFacts.VideoPID == 0 {
		t.Fatalf("expected video PID in variant ring facts")
	}
	if len(vFacts.AudioTracks) > 0 {
		if vFacts.AudioTracks[0].Codec != "aac" {
			t.Fatalf("expected variant audio codec to be aac, got %s", vFacts.AudioTracks[0].Codec)
		}
	}
}
