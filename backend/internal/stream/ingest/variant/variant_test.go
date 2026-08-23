// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"testing"

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
