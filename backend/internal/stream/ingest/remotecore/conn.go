// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// conn carries one request at a time over a stream socket.
//
// One at a time is the whole concurrency model. The ring serialises callers
// through ingestMu already, so a second in-flight request could not arrive, and
// building for one would mean request routing, a reader goroutine and a pending
// map that nothing yet needs. When that changes, the request id in the header is
// already there to make it possible.
type conn struct {
	mu     sync.Mutex
	c      net.Conn
	nextID uint32
}

func newConn(c net.Conn) *conn { return &conn{c: c} }

// roundTrip sends one request and waits for its answer.
//
// The deadline is not advisory. A context that expires while this is blocked in
// read or write has to end the call, and on a socket the only thing that does
// that is a deadline on the connection itself - so one is set from the context,
// and a watcher forces one the moment the context is done. Without the watcher a
// cancellation with no deadline attached would wait forever on a peer that never
// answers, which is exactly the peer this exists to survive.
func (t *conn) roundTrip(ctx context.Context, req Frame) (Frame, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}

	if dl, ok := ctx.Deadline(); ok {
		_ = t.c.SetDeadline(dl)
	} else {
		_ = t.c.SetDeadline(time.Time{})
	}

	// Unblocks a read or write that is already in progress when the context ends.
	// Setting a deadline in the past makes the in-flight call return immediately.
	//
	// Joined, not just signalled. Closing stop only tells the watcher to leave; it
	// may still be inside the select and go on to set a deadline in the past. If
	// that lands after the next round trip has set its own, request N+1 dies of
	// request N's context - and the caller sees a core that times out for no
	// reason it can see. #896 makes that likely rather than theoretical: the
	// context it derives per chunk is cancelled the moment the call returns.
	stop := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			_ = t.c.SetDeadline(time.Unix(1, 0))
		case <-stop:
		}
	}()
	defer func() {
		close(stop)
		<-watchDone
	}()

	req.Version = Version
	req.RequestID = t.nextID
	t.nextID++

	raw, err := req.Encode()
	if err != nil {
		return Frame{}, err
	}
	if _, err := t.c.Write(raw); err != nil {
		return Frame{}, t.classify(ctx, err)
	}

	resp, err := t.readFrame(ctx)
	if err != nil {
		return Frame{}, err
	}

	// Version first, and on its own. If the version is wrong then every field
	// after it means whatever that version says it means - so a mismatched id in
	// the same frame is not evidence of anything, and reporting it would send the
	// caller after the wrong problem.
	if resp.Version != Version {
		return Frame{}, fmt.Errorf("%w: peer answered with version %d, this build speaks %d",
			mediafacts.ErrCoreProtocolVersion, resp.Version, Version)
	}
	// An answer to a different question is not an answer, even when the id
	// matches. Without this a peer could reply to Ingest with a SetTargetProgram
	// frame whose body happens to be the right shape, and the caller would read
	// a result out of it.
	if resp.Type != req.Type {
		return Frame{}, fmt.Errorf("%w: asked type %d, answered type %d",
			mediafacts.ErrCoreInvalidResponse, req.Type, resp.Type)
	}
	// A peer that answers a question nobody asked is not one this side can stay in
	// step with: the next answer would belong to the request after that.
	if resp.RequestID != req.RequestID {
		return Frame{}, fmt.Errorf("%w: answer carried request id %d, expected %d",
			mediafacts.ErrCoreInvalidResponse, resp.RequestID, req.RequestID)
	}
	return resp, nil
}

func (t *conn) readFrame(ctx context.Context) (Frame, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(t.c, lenBuf[:]); err != nil {
		return Frame{}, t.classify(ctx, err)
	}

	n := binary.BigEndian.Uint32(lenBuf[:])
	if n < HeaderSize {
		return Frame{}, fmt.Errorf("%w: announced %d bytes, a header is %d",
			mediafacts.ErrCoreInvalidResponse, n, HeaderSize)
	}
	// Checked before allocating. A length prefix from a peer that is failing is
	// not a request for memory.
	if n > MaxFrameSize {
		return Frame{}, fmt.Errorf("%w: peer announced %d bytes", mediafacts.ErrCoreInvalidResponse, n)
	}

	payload := make([]byte, n)
	if _, err := io.ReadFull(t.c, payload); err != nil {
		return Frame{}, t.classify(ctx, err)
	}
	f, err := DecodeHeader(payload)
	if err != nil {
		return Frame{}, fmt.Errorf("%w: %v", mediafacts.ErrCoreInvalidResponse, err)
	}
	return f, nil
}

// classify turns a transport error into one of the contract's failures.
//
// The distinction that matters is why the socket stopped working, because the
// caller logs it - not what happens next, which is the same for all of them and
// decided by the ring.
func (t *conn) classify(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	// The caller's own cancellation is reported as itself. It is not the core's
	// fault, even though the consequence for the core is the same.
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %v", mediafacts.ErrCoreTimeout, err)
		}
		return ctxErr
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: %v", mediafacts.ErrCoreTimeout, err)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("%w: %v", mediafacts.ErrCoreGone, err)
	}
	return fmt.Errorf("%w: %v", mediafacts.ErrCoreGone, err)
}

func (t *conn) close() error { return t.c.Close() }
