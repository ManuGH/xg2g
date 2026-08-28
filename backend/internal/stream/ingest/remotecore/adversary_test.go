// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// These come first, before any test that the protocol works, because the peer
// this has to survive is not the one that behaves. A core that answers correctly
// is the easy half; everything below is what a process on the other end of a
// socket actually does when it is broken.

const adversaryWait = 3 * time.Second

func peer(t *testing.T) (*conn, net.Conn) {
	t.Helper()
	ours, theirs := net.Pipe()
	t.Cleanup(func() {
		_ = ours.Close()
		_ = theirs.Close()
	})
	return newConn(ours), theirs
}

func readRequest(t *testing.T, c net.Conn) Frame {
	t.Helper()
	var lenBuf [4]byte
	if _, err := c.Read(lenBuf[:]); err != nil {
		return Frame{}
	}
	payload := make([]byte, binary.BigEndian.Uint32(lenBuf[:]))
	total := 0
	for total < len(payload) {
		n, err := c.Read(payload[total:])
		if err != nil {
			return Frame{}
		}
		total += n
	}
	f, err := DecodeHeader(payload)
	if err != nil {
		return Frame{}
	}
	return f
}

func answer(t *testing.T, c net.Conn, f Frame) {
	t.Helper()
	raw, err := f.Encode()
	if err != nil {
		return
	}
	_, _ = c.Write(raw)
}

func call(ctx context.Context, c *conn) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		_, err := c.roundTrip(ctx, Frame{Type: MsgIngest, Body: make([]byte, 8)})
		errCh <- err
	}()
	return errCh
}

func expect(t *testing.T, errCh <-chan error, want error) {
	t.Helper()
	select {
	case err := <-errCh:
		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
	case <-time.After(adversaryWait):
		t.Fatal("the call never returned; a misbehaving peer pinned the caller")
	}
}

// The peer that says nothing at all. Without a deadline that reaches the socket
// this is where a caller waits forever.
func TestAdversary_NeverAnswers(t *testing.T) {
	c, far := peer(t)
	go readRequest(t, far)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	expect(t, call(ctx, c), mediafacts.ErrCoreTimeout)
}

// An answer that arrives after the deadline is not an answer.
func TestAdversary_AnswersAfterTheDeadline(t *testing.T) {
	c, far := peer(t)
	go func() {
		req := readRequest(t, far)
		time.Sleep(300 * time.Millisecond)
		answer(t, far, Frame{Version: Version, Type: req.Type, RequestID: req.RequestID, Body: []byte{StatusOK}})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	expect(t, call(ctx, c), mediafacts.ErrCoreTimeout)
}

// The socket closes mid-conversation.
func TestAdversary_ClosesTheSocket(t *testing.T) {
	c, far := peer(t)
	go func() {
		readRequest(t, far)
		_ = far.Close()
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreGone)
}

// A frame that promises more than it delivers, then goes away.
func TestAdversary_SendsAPartialFrame(t *testing.T) {
	c, far := peer(t)
	go func() {
		readRequest(t, far)
		_, _ = far.Write([]byte{0x00, 0x00, 0x00, 0x20, 0x01, 0x02})
		_ = far.Close()
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreGone)
}

// A length prefix is not an allocation instruction.
func TestAdversary_AnnouncesAnOversizedFrame(t *testing.T) {
	c, far := peer(t)
	go func() {
		readRequest(t, far)
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], MaxFrameSize+1)
		_, _ = far.Write(hdr[:])
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreInvalidResponse)
}

// A frame shorter than its own header.
func TestAdversary_AnnouncesAFrameTooShortToBeOne(t *testing.T) {
	c, far := peer(t)
	go func() {
		readRequest(t, far)
		_, _ = far.Write([]byte{0x00, 0x00, 0x00, 0x02, 0x01, 0x02})
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreInvalidResponse)
}

// A version this build does not speak. Fatal, and fatal early.
func TestAdversary_AnswersWithAnotherProtocolVersion(t *testing.T) {
	c, far := peer(t)
	go func() {
		req := readRequest(t, far)
		answer(t, far, Frame{Version: Version + 1, Type: req.Type, RequestID: req.RequestID, Body: []byte{StatusOK}})
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreProtocolVersion)
}

// An answer to a question nobody asked. The danger is not this answer - it is
// that the next one would belong to the request after it, and the two sides would
// stay one message out of step for as long as the connection lives.
func TestAdversary_AnswersTheWrongRequest(t *testing.T) {
	c, far := peer(t)
	go func() {
		req := readRequest(t, far)
		answer(t, far, Frame{Version: Version, Type: req.Type, RequestID: req.RequestID + 99, Body: []byte{StatusOK}})
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreInvalidResponse)
}

// A peer that stops reading. The caller is blocked in write, not in read, and the
// deadline has to reach that too.
func TestAdversary_StopsReading(t *testing.T) {
	c, _ := peer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := c.roundTrip(ctx, Frame{Type: MsgIngest, Body: make([]byte, 1<<20)})
		errCh <- err
	}()
	expect(t, errCh, mediafacts.ErrCoreTimeout)
}

// A caller that gives up with no deadline attached. Without something that forces
// the socket to give up too, this waits for a peer that will never speak.
func TestAdversary_CallerCancelsWithNoDeadline(t *testing.T) {
	c, far := peer(t)
	go readRequest(t, far)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := call(ctx, c)

	time.Sleep(50 * time.Millisecond)
	cancel()
	expect(t, errCh, context.Canceled)
}
