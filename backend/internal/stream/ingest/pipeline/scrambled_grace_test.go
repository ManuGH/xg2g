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
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// repeatReader serves the same bytes forever at roughly broadcast rate, which is
// how a stream that never gets descrambled looks to an attach: packets keep
// coming, none of them clear. The pacing is not cosmetic - an unpaced infinite
// reader overruns the normalizer's staging buffer instead of testing the attach.
type repeatReader struct {
	data []byte
	off  int
}

const repeatChunk = 32 << 10

func (r *repeatReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(25 * time.Millisecond) // ~10 Mbit/s at repeatChunk per read
	limit := len(p)
	if limit > repeatChunk {
		limit = repeatChunk
	}
	n := copy(p[:limit], r.data[r.off:])
	r.off += n
	if r.off >= len(r.data) {
		r.off = 0
	}
	return n, nil
}

func gracePipelineConfig() ConnectorConfig {
	cfg := DefaultConnectorConfig("http://127.0.0.1", 8001)
	cfg.NormConfig.StartupReservoirMs = 50.0
	cfg.NormConfig.PacerIntervalMs = 5.0
	cfg.NormConfig.InitialBitrateKbps = 20000.0
	return cfg
}

func loadCapture(t *testing.T) []byte {
	t.Helper()
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "backend", "testdata", "segments", "verify_final_v3.ts"))
	if err != nil {
		t.Fatalf("read capture failed: %v", err)
	}
	return data
}

// After the receiver rebuilds a program's CA PMT, its descrambler needs seconds of
// ECM round trips to get the control word back, and every packet until then is
// scrambled. An attach that calls that final on the first hundred packets turns a
// stream that is about to come up into a start that never happens - which is what
// a retry right after a lost session used to hit.
func TestPrimedAttach_WaitsOutADescramblerThatComesUp(t *testing.T) {
	clear := loadCapture(t)
	scrambledHead := scrambleVideoPID(t, clear, videoPIDOf(t, clear))
	if len(scrambledHead) > 600<<10 {
		scrambledHead = scrambledHead[:600<<10]
	}

	cfg := gracePipelineConfig()
	pipe, err := NewSessionPipeline(cfg.NormConfig, cfg.RingCapacity, 1)
	if err != nil {
		t.Fatalf("NewSessionPipeline failed: %v", err)
	}
	defer pipe.Close()

	upstream := io.MultiReader(bytes.NewReader(scrambledHead), bytes.NewReader(clear))
	pipe.Start(context.Background(), io.NopCloser(upstream))

	_, reader, err := pipe.PrimedAttachWithTimeout(context.Background(), 10*time.Second)
	if err != nil {
		t.Fatalf("attach failed on a stream that became clear within the grace: %v", err)
	}
	defer func() { _ = reader.Close() }()
}

// The grace is a wait, not a suspension of the verdict: a stream that stays
// scrambled still ends as ErrScrambledStream, and never past the caller's own
// timeout - the caller's budget is what bounds it.
func TestPrimedAttach_ScrambledStaysTerminalWithinTheCallersBudget(t *testing.T) {
	clear := loadCapture(t)
	scrambled := scrambleVideoPID(t, clear, videoPIDOf(t, clear))

	cfg := gracePipelineConfig()
	pipe, err := NewSessionPipeline(cfg.NormConfig, cfg.RingCapacity, 1)
	if err != nil {
		t.Fatalf("NewSessionPipeline failed: %v", err)
	}
	defer pipe.Close()

	pipe.Start(context.Background(), io.NopCloser(&repeatReader{data: scrambled}))

	const budget = 1500 * time.Millisecond
	start := time.Now()
	_, _, err = pipe.PrimedAttachWithTimeout(context.Background(), budget)
	elapsed := time.Since(start)

	if !errors.Is(err, ring.ErrScrambledStream) {
		t.Fatalf("attach on a permanently scrambled stream = %v, want %v", err, ring.ErrScrambledStream)
	}
	if elapsed > budget+time.Second {
		t.Fatalf("attach took %v, which is past the caller's budget of %v", elapsed, budget)
	}
	if elapsed >= ScrambledVerdictGrace {
		t.Fatalf("attach waited %v, but the grace must never outlast the caller's budget of %v", elapsed, budget)
	}
}
