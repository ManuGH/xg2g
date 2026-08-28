// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
	"github.com/ManuGH/xg2g/internal/stream/ingest/variant"
)

// passthroughFFmpeg puts a stand-in ffmpeg on PATH that ignores its arguments and
// copies stdin to stdout, so the variant ring receives the same indexable capture
// the master ring holds. Worker lifetime is what is under test, not encoding, and a
// real ffmpeg would make this skip in the PR gate.
func passthroughFFmpeg(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nexec cat\n"), 0o700); err != nil {
		t.Fatalf("write stand-in ffmpeg: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// feedRingDirectly replays a real capture straight into the master ring until the
// test ends. Straight in, rather than through the connector: the normalizer's pacing
// has its own tests, and this one is about how long a variant worker lives.
func feedRingDirectly(t *testing.T, master *ring.MasterRing) {
	t.Helper()

	capture := tsfixture.Load(t, "verify_final_v3.ts")

	stop := make(chan struct{})
	done := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		<-done
	})

	go func() {
		defer close(done)
		const chunk = 64 * ring.TSPacketSize
		for {
			for i := 0; i+chunk <= len(capture); i += chunk {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := master.Push(context.Background(), capture[i:i+chunk]); err != nil {
					return
				}
				time.Sleep(time.Millisecond)
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

// The ownership rule at the level a client actually meets it.
//
// Two clients coalesce onto one variant worker. The first disconnects - which in
// production is exactly an HTTP request context being cancelled - and the second
// must keep receiving bytes. The worker belongs to the pipeline, not to whichever
// subscriber happened to create it.
func TestPipeline_VariantWorkerSurvivesFirstClientDisconnect(t *testing.T) {
	passthroughFFmpeg(t)

	normCfg := normalizer.DefaultConfig()
	normCfg.StartupReservoirMs = 0.0

	pipe, err := NewSessionPipeline(normCfg, 20000*ring.TSPacketSize, 0)
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	defer pipe.Close()

	feedRingDirectly(t, pipe.MasterRing())

	if _, reader, aerr := pipe.PrimedAttachWithTimeout(context.Background(), 10*time.Second); aerr != nil {
		t.Fatalf("master stream never became attachable: %v", aerr)
	} else {
		_ = reader.Close()
	}

	key := variant.StreamVariantKey{
		StreamFingerprint: "two-client-lifetime",
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

	// Client 1 arrives on its own request context.
	firstRequest, disconnectFirst := context.WithCancel(context.Background())
	defer disconnectFirst()
	_, firstReader, releaseFirst, err := pipe.AttachAudioVariantWithTimeout(firstRequest, key, 10*time.Second)
	if err != nil {
		t.Fatalf("first client failed to attach to the variant: %v", err)
	}

	// Client 2 coalesces onto the same worker.
	secondRequest, disconnectSecond := context.WithCancel(context.Background())
	defer disconnectSecond()
	_, secondReader, releaseSecond, err := pipe.AttachAudioVariantWithTimeout(secondRequest, key, 10*time.Second)
	if err != nil {
		t.Fatalf("second client failed to attach to the variant: %v", err)
	}
	defer func() { _ = secondReader.Close() }()
	defer releaseSecond()

	if got := pipe.AudioVariants().ActiveWorkerCount(); got != 1 {
		t.Fatalf("the two clients did not share one worker: %d workers active", got)
	}
	if n, rerr := readWithin(t, secondReader, 10*time.Second); rerr != nil || n == 0 {
		t.Fatalf("no variant bytes before any disconnect: n=%d err=%v", n, rerr)
	}

	// Client 1 disconnects: reader closed, hold released, request context cancelled.
	_ = firstReader.Close()
	releaseFirst()
	disconnectFirst()

	// Client 2 must keep streaming.
	deadline := time.Now().Add(2 * time.Second)
	total := 0
	for time.Now().Before(deadline) {
		n, rerr := readWithin(t, secondReader, 10*time.Second)
		if rerr != nil {
			t.Fatalf("the second client's stream ended when the first disconnected: %v (bytes since: %d)", rerr, total)
		}
		total += n
	}
	if total == 0 {
		t.Fatal("the second client received no bytes after the first client disconnected")
	}
	if got := pipe.AudioVariants().ActiveWorkerCount(); got != 1 {
		t.Fatalf("the shared worker is no longer active: %d workers", got)
	}
}
