// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The core is about to become a process on the other end of a socket. These tests
// are about what happens to the ring when that process is slow, wrong, or gone,
// and they are written against the in-process core because the answer must not
// depend on which implementation is behind the boundary.

const isolationWait = 2 * time.Second

// mustReturnWithin fails the test if f is still running after d. Every call it
// guards is one that has to stay reachable while a core is working.
func mustReturnWithin(t *testing.T, what string, f func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()

	select {
	case <-done:
	case <-time.After(isolationWait):
		t.Fatalf("%s did not return within %v while a core was running", what, isolationWait)
	}
}

// blockingCore stops inside Ingest until it is released, which is what a hung
// socket looks like from the ring's side.
type blockingCore struct {
	mediafacts.Core
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int32
}

func newBlockingCore(inner mediafacts.Core) *blockingCore {
	return &blockingCore{Core: inner, entered: make(chan struct{}), release: make(chan struct{})}
}

func (c *blockingCore) Ingest(startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	c.calls.Add(1)
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.Core.Ingest(startOffset, data)
}

func onePacket() []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x1F
	pkt[2] = 0xFF
	pkt[3] = 0x10
	return pkt
}

// 1. A core that never returns must cost the stream its facts, not its readers.
// Before the boundary moved, Ingest ran under the lock every reader takes, so a
// hung core would have taken subscribers, readiness and Close down with it.
func TestIsolation_ASlowCoreDoesNotBlockReadersOrClose(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	core := newBlockingCore(r.core)
	r.core = core

	pushErr := make(chan error, 1)
	go func() { _, err := r.Push(onePacket()); pushErr <- err }()

	<-core.entered

	mustReturnWithin(t, "ReadinessFacts", func() { _ = r.ReadinessFacts() })
	mustReturnWithin(t, "Head", func() { _ = r.Head() })
	mustReturnWithin(t, "Tail", func() { _ = r.Tail() })
	mustReturnWithin(t, "BufferedBytes", func() { _ = r.BufferedBytes() })
	mustReturnWithin(t, "Generation", func() { _ = r.Generation() })
	mustReturnWithin(t, "KeyframeOffsets", func() { _ = r.KeyframeOffsets() })
	mustReturnWithin(t, "Close", r.Close)

	close(core.release)

	select {
	case err := <-pushErr:
		if !errors.Is(err, ErrRingClosed) {
			t.Errorf("Push err = %v, want ErrRingClosed - the ring closed while the chunk was in the core", err)
		}
	case <-time.After(isolationWait):
		t.Fatal("Push never returned after the core was released")
	}
}

// countingCore records how many callers are inside Ingest at once and the offset
// each of them was given.
type countingCore struct {
	mediafacts.Core

	inFlight    atomic.Int32
	maxInFlight atomic.Int32

	mu      sync.Mutex
	offsets []int64
}

func (c *countingCore) Ingest(startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	now := c.inFlight.Add(1)
	for {
		max := c.maxInFlight.Load()
		if now <= max || c.maxInFlight.CompareAndSwap(max, now) {
			break
		}
	}
	defer c.inFlight.Add(-1)

	// Long enough that genuinely concurrent callers would overlap here.
	time.Sleep(time.Millisecond)

	c.mu.Lock()
	c.offsets = append(c.offsets, startOffset)
	c.mu.Unlock()

	return c.Core.Ingest(startOffset, data)
}

// 2. The core is single-threaded by contract, and the bytes have to land in the
// order they were interpreted. Both come from the same lock: whoever holds it
// reads the head, interprets against it, and commits at it before the next
// writer sees anything.
func TestIsolation_WritersAreSerialisedAndKeepTheirOrder(t *testing.T) {
	const writers = 8

	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()
	core := &countingCore{Core: r.core}
	r.core = core

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Push(onePacket()); err != nil {
				t.Errorf("Push: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := core.maxInFlight.Load(); got != 1 {
		t.Errorf("%d callers were inside the core at once, want 1 - the core is single-threaded by contract", got)
	}

	core.mu.Lock()
	offsets := append([]int64(nil), core.offsets...)
	core.mu.Unlock()

	if len(offsets) != writers {
		t.Fatalf("core saw %d chunks, want %d", len(offsets), writers)
	}

	// Every chunk was measured against the head the previous one left behind: the
	// offsets are the whole sequence, once each, with no gap and no overlap.
	seen := make(map[int64]bool, writers)
	for _, off := range offsets {
		if off%int64(TSPacketSize) != 0 || off < 0 || off >= int64(writers*TSPacketSize) {
			t.Errorf("chunk offset %d is not one of the %d packet positions", off, writers)
		}
		if seen[off] {
			t.Errorf("two chunks were both interpreted at offset %d", off)
		}
		seen[off] = true
	}
	if got := r.Head(); got != int64(writers*TSPacketSize) {
		t.Errorf("Head = %d, want %d - a chunk was committed at the wrong offset", got, writers*TSPacketSize)
	}
}

// erroringCore fails the way a dead socket would.
type erroringCore struct {
	mediafacts.Core
	err   error
	calls atomic.Int32
}

func (c *erroringCore) Ingest(startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	c.calls.Add(1)
	return mediafacts.ParseResult{}, c.err
}

var errCoreExploded = errors.New("core exploded")

// 3. A core that failed once has consumed bytes the ring then threw away, so its
// state and the ring's have parted company. Asking it again would be asking a
// stream nobody is holding.
func TestIsolation_AFailedCoreIsNotReused(t *testing.T) {
	tests := []struct {
		name    string
		core    func(inner mediafacts.Core) mediafacts.Core
		wantErr error
	}{
		{
			name:    "the core errored",
			core:    func(inner mediafacts.Core) mediafacts.Core { return &erroringCore{Core: inner, err: errCoreExploded} },
			wantErr: errCoreExploded,
		},
		{
			name:    "the core came back short",
			core:    func(inner mediafacts.Core) mediafacts.Core { return shortCore{Core: inner, shortBy: TSPacketSize} },
			wantErr: ErrCoreIncomplete,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMasterRing(400 * TSPacketSize)
			defer r.Close()
			r.core = tc.core(r.core)

			headBefore, genBefore := r.Head(), r.Generation()

			if _, err := r.Push(onePacket()); !errors.Is(err, tc.wantErr) {
				t.Fatalf("first Push err = %v, want %v", err, tc.wantErr)
			}
			if got := r.Head(); got != headBefore {
				t.Errorf("Head = %d, want %d", got, headBefore)
			}
			if got := r.Generation(); got != genBefore {
				t.Errorf("Generation = %d, want %d", got, genBefore)
			}
			if got := r.BufferedBytes(); got != 0 {
				t.Errorf("BufferedBytes = %d, want 0", got)
			}

			// The second attempt must not reach the core at all.
			if _, err := r.Push(onePacket()); !errors.Is(err, ErrCoreUnusable) {
				t.Errorf("second Push err = %v, want ErrCoreUnusable", err)
			}
			if got := r.Head(); got != headBefore {
				t.Errorf("Head = %d after the refused second chunk, want %d", got, headBefore)
			}
		})
	}
}

// 4a. Close during an ingest. The chunk comes back to a ring that is no longer
// accepting anything, and publishing into it would hand bytes to a stream that
// has already been torn down.
func TestIsolation_CloseDuringIngestPreventsAStaleCommit(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	core := newBlockingCore(r.core)
	r.core = core

	headBefore, genBefore := r.Head(), r.Generation()

	pushErr := make(chan error, 1)
	go func() { _, err := r.Push(onePacket()); pushErr <- err }()

	<-core.entered
	r.Close()
	close(core.release)

	select {
	case err := <-pushErr:
		if !errors.Is(err, ErrRingClosed) {
			t.Fatalf("Push err = %v, want ErrRingClosed", err)
		}
	case <-time.After(isolationWait):
		t.Fatal("Push never returned")
	}

	if got := r.Head(); got != headBefore {
		t.Errorf("Head = %d, want %d - the chunk was committed after Close", got, headBefore)
	}
	if got := r.Generation(); got != genBefore {
		t.Errorf("Generation = %d, want %d", got, genBefore)
	}
	if got := r.BufferedBytes(); got != 0 {
		t.Errorf("BufferedBytes = %d, want 0", got)
	}
}

// 4b. The same rule for the other thing that can change under a chunk: the ring
// moving on. What the core read describes bytes at one offset, and committing it
// at another is a plausible, silent lie.
func TestIsolation_ARingThatMovedUnderTheChunkRefusesTheCommit(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	defer r.Close()
	core := newBlockingCore(r.core)
	r.core = core

	pushErr := make(chan error, 1)
	go func() { _, err := r.Push(onePacket()); pushErr <- err }()

	<-core.entered

	// Something else advanced the ring while the chunk was being read.
	r.mu.Lock()
	r.head += TSPacketSize
	r.mu.Unlock()

	close(core.release)

	select {
	case err := <-pushErr:
		if !errors.Is(err, ErrRingAdvanced) {
			t.Fatalf("Push err = %v, want ErrRingAdvanced", err)
		}
	case <-time.After(isolationWait):
		t.Fatal("Push never returned")
	}

	if got := r.Head(); got != TSPacketSize {
		t.Errorf("Head = %d, want %d - the chunk was committed at the wrong offset", got, TSPacketSize)
	}

	// The core interpreted that chunk and the ring threw it away, which is the
	// same divergence an error or a short result leaves behind. Asking it again
	// would be asking about a stream nobody is holding.
	entered := core.calls.Load()
	if _, err := r.Push(onePacket()); !errors.Is(err, ErrCoreUnusable) {
		t.Errorf("second Push err = %v, want ErrCoreUnusable", err)
	}
	if got := core.calls.Load(); got != entered {
		t.Errorf("the core was entered %d more times after it diverged from the ring, want 0", got-entered)
	}
}
