// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The contract a remote core will have to keep. Every case here is written
// against the in-process core, on purpose: if the rules only hold once a socket
// is involved, they are not rules, they are behaviour of one implementation.

// scriptedCore fails on demand and counts how often it was entered.
type scriptedCore struct {
	mediafacts.Core
	err    error
	delay  time.Duration
	visits atomic.Int32
}

func (c *scriptedCore) Ingest(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	c.visits.Add(1)
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return mediafacts.ParseResult{}, ctx.Err()
		}
	}
	if c.err != nil {
		return mediafacts.ParseResult{}, c.err
	}
	return c.Core.Ingest(ctx, startOffset, data)
}

// A caller that gave up before the core was entered has not damaged anything.
// Nothing was interpreted, so the core and the ring still describe the same
// stream, and retiring it here would throw away a working core over a caller's
// timeout.
func TestContract_CancellationBeforeTheCoreLeavesItUsable(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	defer r.Close()
	core := &scriptedCore{Core: r.core}
	r.core = core

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := r.Push(ctx, onePacket()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Push err = %v, want context.Canceled", err)
	}
	if got := core.visits.Load(); got != 0 {
		t.Errorf("core entered %d times for a chunk that was refused before it, want 0", got)
	}

	// The core survived, so the next chunk goes through.
	if _, err := r.Push(context.Background(), onePacket()); err != nil {
		t.Errorf("second Push err = %v, want nil - the core was never damaged", err)
	}
	if got := r.Head(); got != TSPacketSize {
		t.Errorf("Head = %d, want %d", got, TSPacketSize)
	}
}

// Once the core has been entered, it has consumed something the ring is about to
// throw away - and it makes no difference whether the caller gave up or the core
// went quiet. Both leave the two out of step.
func TestContract_CancellationInsideTheCoreRetiresIt(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	defer r.Close()
	core := &scriptedCore{Core: r.core, delay: 5 * time.Second}
	r.core = core

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	headBefore := r.Head()
	_, err := r.Push(ctx, onePacket())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Push err = %v, want context.Canceled", err)
	}
	if got := r.Head(); got != headBefore {
		t.Errorf("Head = %d, want %d - a cancelled chunk was committed", got, headBefore)
	}

	entered := core.visits.Load()
	if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, ErrCoreUnusable) {
		t.Errorf("second Push err = %v, want ErrCoreUnusable", err)
	}
	if got := core.visits.Load(); got != entered {
		t.Errorf("core entered %d more times after it diverged, want 0", got-entered)
	}
}

// The ring's own deadline is not the caller's. When it is what expired, the
// caller is told the core went quiet rather than that its own context did.
func TestContract_TheRingsDeadlineIsReportedAsACoreTimeout(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	defer r.Close()
	r.ingestDeadline = 40 * time.Millisecond
	core := &scriptedCore{Core: r.core, delay: 5 * time.Second}
	r.core = core

	_, err := r.Push(context.Background(), onePacket())
	if !errors.Is(err, mediafacts.ErrCoreTimeout) {
		t.Fatalf("Push err = %v, want ErrCoreTimeout", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Push err = %v, want it to still carry the deadline it came from", err)
	}
	if got := r.Head(); got != 0 {
		t.Errorf("Head = %d, want 0", got)
	}
	if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, ErrCoreUnusable) {
		t.Errorf("second Push err = %v, want ErrCoreUnusable", err)
	}
}

// However a core dies, the consequence is the same. This is the rule the sidecar
// will be held to, written as a table so a new failure mode has an obvious place
// to be added - and no place to be handled differently.
func TestContract_EveryCoreFailureRetiresTheCore(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"the core went quiet", mediafacts.ErrCoreTimeout},
		{"the core is gone", mediafacts.ErrCoreGone},
		{"the core crashed", mediafacts.ErrCoreCrashed},
		{"the core answered with nonsense", mediafacts.ErrCoreInvalidResponse},
		{"the core speaks another protocol", mediafacts.ErrCoreProtocolVersion},
		{"something nobody has named yet", errors.New("unclassified")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMasterRing(400 * TSPacketSize)
			defer r.Close()
			core := &scriptedCore{Core: r.core, err: tc.err}
			r.core = core

			gen, head := r.Generation(), r.Head()

			if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, tc.err) {
				t.Fatalf("Push err = %v, want %v", err, tc.err)
			}
			if got := r.Head(); got != head {
				t.Errorf("Head = %d, want %d", got, head)
			}
			if got := r.Generation(); got != gen {
				t.Errorf("Generation = %d, want %d", got, gen)
			}
			if got := r.BufferedBytes(); got != 0 {
				t.Errorf("BufferedBytes = %d, want 0", got)
			}

			entered := core.visits.Load()
			if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, ErrCoreUnusable) {
				t.Errorf("second Push err = %v, want ErrCoreUnusable", err)
			}
			if got := core.visits.Load(); got != entered {
				t.Errorf("core entered %d more times, want 0", got-entered)
			}
		})
	}
}

// SetTargetProgram reaches the core too, and an implementation behind a socket
// can hang there just as easily. It is bounded and it retires the core the same
// way - discovered here rather than during process integration.
func TestContract_SetTargetProgramIsBoundedAndRetiresTheCore(t *testing.T) {
	t.Run("cancelled before the core is entered", func(t *testing.T) {
		r := NewMasterRing(400 * TSPacketSize)
		defer r.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := r.SetTargetProgram(ctx, 2); !errors.Is(err, context.Canceled) {
			t.Fatalf("SetTargetProgram err = %v, want context.Canceled", err)
		}
		if _, err := r.Push(context.Background(), onePacket()); err != nil {
			t.Errorf("Push err = %v, want nil - nothing was damaged", err)
		}
	})

	t.Run("the core fails inside it", func(t *testing.T) {
		r := NewMasterRing(400 * TSPacketSize)
		defer r.Close()
		r.core = &failingTargetCore{Core: r.core}

		if err := r.SetTargetProgram(context.Background(), 2); !errors.Is(err, mediafacts.ErrCoreGone) {
			t.Fatalf("SetTargetProgram err = %v, want ErrCoreGone", err)
		}
		if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, ErrCoreUnusable) {
			t.Errorf("Push err = %v, want ErrCoreUnusable", err)
		}
	})
}

type failingTargetCore struct{ mediafacts.Core }

func (c *failingTargetCore) SetTargetProgram(context.Context, uint16) (mediafacts.ParseResult, error) {
	return mediafacts.ParseResult{}, mediafacts.ErrCoreGone
}
