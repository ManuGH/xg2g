// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// shadowOnPipe is a RemoteAudioShadow whose peer is the other end of a pipe.
//
// No process, because these tests are about the conversation rather than the
// lifecycle: what this side does when the peer misbehaves, before anything has to
// be signalled or reaped. The process cases run against a real child over a real
// socket - see the shadow tests in process_test.go, which prove the same
// cancellation with a peer that can be left behind if it goes wrong.
func shadowOnPipe(t *testing.T) (*RemoteAudioShadow, net.Conn) {
	t.Helper()
	ours, theirs := net.Pipe()
	s := &RemoteAudioShadow{peer: &RemoteCore{conn: newConn(ours), waitDone: make(chan struct{})}}
	t.Cleanup(func() {
		_ = s.Close()
		_ = theirs.Close()
	})
	return s, theirs
}

func shadowRequest(t *testing.T, c net.Conn) (Frame, []mediafacts.AudioShadowBatch) {
	t.Helper()
	var lenBuf [4]byte
	if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
		t.Fatalf("read length: %v", err)
	}
	payload := make([]byte, binary.BigEndian.Uint32(lenBuf[:]))
	if _, err := io.ReadFull(c, payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	f, err := DecodeHeader(payload)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	batches, err := decodeObserveAudioRequest(f.Body)
	if err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return f, batches
}

func shadowAnswer(t *testing.T, c net.Conn, f Frame, body []byte) {
	t.Helper()
	raw, err := Frame{Version: Version, Type: f.Type, RequestID: f.RequestID, Body: body}.Encode()
	if err != nil {
		t.Fatalf("encode answer: %v", err)
	}
	if _, err := c.Write(raw); err != nil {
		t.Errorf("write answer: %v", err)
	}
}

// oneBatch is the question every test below asks, so that what differs between
// them is only the answer.
func oneBatch() []mediafacts.AudioShadowBatch {
	return []mediafacts.AudioShadowBatch{
		{PID: 300, Epoch: 1, Feeds: [][]byte{{0x0B, 0x77}, {0x01}}},
		{PID: 301, Epoch: 1, Feeds: [][]byte{{0x02}}},
	}
}

func observeInBackground(s *RemoteAudioShadow, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := s.ObserveAudio(ctx, oneBatch())
		done <- err
	}()
	return done
}

// expectReturn is the property every adversarial case below is a variation of:
// the call comes back, on its own, without the peer having cooperated.
func expectReturn(t *testing.T, done <-chan error, within time.Duration, what string) error {
	t.Helper()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("%s: ObserveAudio succeeded", what)
		}
		return err
	case <-time.After(within):
		t.Fatalf("%s: ObserveAudio did not return within %v - the peer decided how long this side waits", what, within)
		return nil
	}
}

func TestShadow_AnHonestPeerIsUnderstood(t *testing.T) {
	s, peer := shadowOnPipe(t)

	go func() {
		f, batches := shadowRequest(t, peer)
		out := make([]mediafacts.AudioShadowObservation, len(batches))
		for i, b := range batches {
			out[i] = mediafacts.AudioShadowObservation{
				PID:   b.PID,
				Epoch: b.Epoch,
				// Something that depends on the feeds, so a peer that ignored them
				// could not produce it.
				Observation: esaudio.Observation{Frames: uint64(len(b.Feeds))},
			}
		}
		body, err := encodeObserveAudioAnswer(out)
		if err != nil {
			t.Errorf("encode answer: %v", err)
			return
		}
		shadowAnswer(t, peer, f, body)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := s.ObserveAudio(ctx, oneBatch())
	if err != nil {
		t.Fatalf("ObserveAudio: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d observations, asked about 2", len(got))
	}
	if got[0].PID != 300 || got[0].Observation.Frames != 2 || got[1].PID != 301 || got[1].Observation.Frames != 1 {
		t.Errorf("the answer does not describe what was asked: %+v", got)
	}
}

// A. The peer takes the request and never says anything again.
func TestShadow_APeerThatNeverAnswersDoesNotHoldTheCall(t *testing.T) {
	s, peer := shadowOnPipe(t)
	go func() { shadowRequest(t, peer) }()

	ctx, cancel := context.WithCancel(context.Background())
	done := observeInBackground(s, ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()

	err := expectReturn(t, done, 2*time.Second, "a peer that never answers")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the caller's own cancellation", err)
	}
}

// A, without a cancellation to lean on: a deadline alone has to do it, because
// that is what a caller who set one is entitled to.
func TestShadow_ADeadlineEndsACallAPeerWillNotFinish(t *testing.T) {
	s, peer := shadowOnPipe(t)
	go func() { shadowRequest(t, peer) }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := expectReturn(t, observeInBackground(s, ctx), 2*time.Second, "a peer that never answers")
	if !errors.Is(err, mediafacts.ErrCoreTimeout) {
		t.Errorf("err = %v, want ErrCoreTimeout", err)
	}
}

// B. The peer stops reading, so it is the write that is stuck rather than the
// read. Nothing on this side may care which.
func TestShadow_APeerThatStopsReadingDoesNotHoldTheCall(t *testing.T) {
	// The peer is never served: nothing on the other end ever reads a byte, so it
	// is the write that is stuck rather than the read.
	s, _ := shadowOnPipe(t)
	batches := []mediafacts.AudioShadowBatch{{PID: 1, Epoch: 1, Feeds: [][]byte{make([]byte, 1<<20)}}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.ObserveAudio(ctx, batches)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	err := expectReturn(t, done, 2*time.Second, "a peer that stops reading")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the caller's own cancellation", err)
	}
}

// C. The peer sends the beginning of an answer and then stops.
func TestShadow_APeerThatSendsHalfAnAnswerDoesNotHoldTheCall(t *testing.T) {
	s, peer := shadowOnPipe(t)
	go func() {
		f, _ := shadowRequest(t, peer)
		// A length prefix promising a whole frame, and then two bytes of it.
		var head [6]byte
		binary.BigEndian.PutUint32(head[:4], uint32(HeaderSize+64))
		head[4], head[5] = Version, f.Type
		if _, err := peer.Write(head[:]); err != nil {
			t.Errorf("write partial: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := observeInBackground(s, ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := expectReturn(t, done, 2*time.Second, "a peer stuck mid-answer")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the caller's own cancellation", err)
	}
}

// D. The peer hangs up.
func TestShadow_APeerThatClosesTheSocketRetiresTheShadow(t *testing.T) {
	s, peer := shadowOnPipe(t)
	go func() {
		shadowRequest(t, peer)
		_ = peer.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.ObserveAudio(ctx, oneBatch()); !errors.Is(err, mediafacts.ErrCoreGone) {
		t.Errorf("err = %v, want ErrCoreGone", err)
	}
	assertRetired(t, s)
}

// E and F. An answer this side cannot believe, in each of the ways it can arrive.
func TestShadow_AnAnswerThatIsNotOneRetiresTheShadow(t *testing.T) {
	cases := []struct {
		name   string
		answer func(batches []mediafacts.AudioShadowBatch) []byte
	}{
		{"one observation missing", func(b []mediafacts.AudioShadowBatch) []byte {
			return mustAnswer(observationsFor(b[:1]))
		}},
		{"one observation too many", func(b []mediafacts.AudioShadowBatch) []byte {
			return mustAnswer(append(observationsFor(b), mediafacts.AudioShadowObservation{PID: 999, Epoch: 1}))
		}},
		{"the answers reordered", func(b []mediafacts.AudioShadowBatch) []byte {
			o := observationsFor(b)
			o[0], o[1] = o[1], o[0]
			return mustAnswer(o)
		}},
		{"an answer about another stream", func(b []mediafacts.AudioShadowBatch) []byte {
			o := observationsFor(b)
			o[1].PID = 4095
			return mustAnswer(o)
		}},
		{"an answer about another epoch", func(b []mediafacts.AudioShadowBatch) []byte {
			o := observationsFor(b)
			o[1].Epoch = 99
			return mustAnswer(o)
		}},
		{"a body that is not an answer", func([]mediafacts.AudioShadowBatch) []byte {
			return []byte{StatusOK, 0x00}
		}},
		{"bytes after the last observation", func(b []mediafacts.AudioShadowBatch) []byte {
			return append(mustAnswer(observationsFor(b)), 0x00)
		}},
		{"a refusal", func([]mediafacts.AudioShadowBatch) []byte {
			return []byte{StatusMalformed}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, peer := shadowOnPipe(t)
			go func() {
				f, batches := shadowRequest(t, peer)
				shadowAnswer(t, peer, f, tc.answer(batches))
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := s.ObserveAudio(ctx, oneBatch()); err == nil {
				t.Fatal("an answer that does not line up was believed")
			}
			assertRetired(t, s)
		})
	}
}

func observationsFor(batches []mediafacts.AudioShadowBatch) []mediafacts.AudioShadowObservation {
	out := make([]mediafacts.AudioShadowObservation, len(batches))
	for i, b := range batches {
		out[i] = mediafacts.AudioShadowObservation{PID: b.PID, Epoch: b.Epoch}
	}
	return out
}

func mustAnswer(o []mediafacts.AudioShadowObservation) []byte {
	body, err := encodeObserveAudioAnswer(o)
	if err != nil {
		panic(err)
	}
	return body
}

// A retirement is permanent, and it is permanent without asking. Nothing is put
// on the wire afterwards - which is checked by there being nobody left to read
// it: a peer that is no longer being served would block this call forever if the
// shadow tried again.
func assertRetired(t *testing.T, s *RemoteAudioShadow) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := s.ObserveAudio(context.Background(), oneBatch())
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrShadowRetired) {
			t.Errorf("a later call gave %v, want ErrShadowRetired", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a call after the failure reached the wire instead of being refused")
	}
}

// The request that cannot be framed is refused here, and refusing it is final:
// the peer's observers would otherwise carry a hole for the rest of the session.
func TestShadow_ARequestPastTheFrameBoundRetiresTheShadowRatherThanTruncating(t *testing.T) {
	s, _ := shadowOnPipe(t)
	batches := []mediafacts.AudioShadowBatch{{PID: 1, Epoch: 1, Feeds: [][]byte{make([]byte, MaxFrameSize)}}}

	if _, err := s.ObserveAudio(context.Background(), batches); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
	assertRetired(t, s)
}

// Nothing of this side is left running once a hung call has been cancelled. The
// watcher that forces the deadline is joined by the round trip it belongs to, and
// this is the test that says so out loud rather than trusting the code to keep
// doing it.
func TestShadow_ACancelledCallLeavesNoGoroutineBehind(t *testing.T) {
	s, peer := shadowOnPipe(t)
	go func() {
		for {
			if _, err := io.ReadFull(peer, make([]byte, 1)); err != nil {
				return
			}
		}
	}()

	// Two readings that agree, rather than one taken at a moment of the runtime's
	// choosing. A goroutine that is on its way out is not a leak, and counting it
	// as one would make this fail for reasons that have nothing to do with the
	// shadow.
	settled := func() int {
		last := runtime.NumGoroutine()
		for i := 0; i < 50; i++ {
			time.Sleep(10 * time.Millisecond)
			now := runtime.NumGoroutine()
			if now == last {
				return now
			}
			last = now
		}
		return last
	}

	before := settled()
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, _ = s.ObserveAudio(ctx, oneBatch())
		cancel()
	}
	after := settled()

	// A handful of slack for the runtime's own goroutines; 20 leaked watchers
	// would be unmistakable against it.
	if after > before+5 {
		t.Errorf("goroutines went from %d to %d across 20 cancelled calls", before, after)
	}
}
