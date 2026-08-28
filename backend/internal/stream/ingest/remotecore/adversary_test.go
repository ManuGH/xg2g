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

// An answer of the right shape to the wrong question. The id matches and the body
// looks plausible; only the type says it is an answer to something else.
func TestAdversary_AnswersWithTheWrongMessageType(t *testing.T) {
	c, far := peer(t)
	go func() {
		req := readRequest(t, far)
		answer(t, far, Frame{
			Version:   Version,
			Type:      MsgSetTargetProgram, // asked for Ingest
			RequestID: req.RequestID,
			Body:      []byte{StatusOK},
		})
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreInvalidResponse)
}

// A version this build does not speak, in a frame that is also wrong in another
// way. The version has to win: once it differs, every other field means whatever
// that version says it means, and reporting the id would send the caller after a
// problem that is not there.
func TestAdversary_AnUnknownVersionIsReportedBeforeAnythingElse(t *testing.T) {
	c, far := peer(t)
	go func() {
		req := readRequest(t, far)
		answer(t, far, Frame{
			Version:   Version + 1,
			Type:      MsgSetTargetProgram,
			RequestID: req.RequestID + 99,
			Body:      nil,
		})
	}()
	expect(t, call(context.Background(), c), mediafacts.ErrCoreProtocolVersion)
}

// The nastiest one, because it looks like an intermittent core.
//
// Each call watches its context so a blocked read or write can be broken out of.
// If that watcher is only signalled and not waited for, it can still be inside
// its select when the call returns - and then set a deadline in the past while
// the *next* call is already using the connection. Request N+1 dies of request
// N's context, and nothing in the logs says why.
//
// #896 makes this the normal case rather than a corner: the context it derives
// for each chunk is cancelled the instant the call returns.
func TestAdversary_AStaleWatcherDoesNotPoisonTheNextRequest(t *testing.T) {
	c, far := peer(t)

	go func() {
		for {
			req := readRequest(t, far)
			if req.Type == 0 {
				return
			}
			answer(t, far, Frame{Version: Version, Type: req.Type, RequestID: req.RequestID, Body: []byte{StatusOK}})
		}
	}()

	// Repeated, because the window is a scheduling one: a single pass can miss it
	// even when the join is absent.
	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := c.roundTrip(ctx, Frame{Type: MsgSetTargetProgram, Body: []byte{0, 1}}); err != nil {
			cancel()
			t.Fatalf("round %d: first call: %v", i, err)
		}
		// Exactly what the ring does the moment a chunk is done with.
		cancel()

		ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
		_, err := c.roundTrip(ctx2, Frame{Type: MsgSetTargetProgram, Body: []byte{0, 1}})
		cancel2()
		if err != nil {
			t.Fatalf("round %d: the call after a cancelled one failed: %v", i, err)
		}
	}
}

// coreOnPipe is a RemoteCore with no process behind it, talking to a socket the
// test controls.
//
// Needed because the body of an answer means different things per message type,
// so the checks on it live in the methods and not in roundTrip. Everything above
// goes through roundTrip and therefore cannot see them - a test that calls
// roundTrip to check a per-message rule tests nothing and fails for its own
// reasons, which is how the first version of the two below was written.
func coreOnPipe(t *testing.T) (*RemoteCore, net.Conn) {
	t.Helper()
	c, far := peer(t)
	return &RemoteCore{conn: c}, far
}

// More than was agreed. Trailing bytes are how a peer built against a later
// protocol looks from here, and reading past them as if they were not there
// would let a version mismatch pass quietly as agreement.
func TestAdversary_AnIngestAnswerWithMoreBodyThanAgreed(t *testing.T) {
	core, far := coreOnPipe(t)
	go func() {
		req := readRequest(t, far)
		body := append([]byte{StatusOK}, make([]byte, 8+4)...) // status + offset + four too many
		answer(t, far, Frame{Version: Version, Type: req.Type, RequestID: req.RequestID, Body: body})
	}()

	errCh := make(chan error, 1)
	go func() {
		_, err := core.Ingest(context.Background(), 0, make([]byte, 188))
		errCh <- err
	}()
	expect(t, errCh, mediafacts.ErrCoreInvalidResponse)
}

// The same for the message whose answer is only a status byte. A peer that
// appends to it is not answering this protocol.
func TestAdversary_ASetTargetAnswerWithMoreBodyThanAgreed(t *testing.T) {
	core, far := coreOnPipe(t)
	go func() {
		req := readRequest(t, far)
		answer(t, far, Frame{
			Version:   Version,
			Type:      req.Type,
			RequestID: req.RequestID,
			Body:      []byte{StatusOK, 0x00},
		})
	}()

	errCh := make(chan error, 1)
	go func() {
		_, err := core.SetTargetProgram(context.Background(), 1)
		errCh <- err
	}()
	expect(t, errCh, mediafacts.ErrCoreInvalidResponse)
}
