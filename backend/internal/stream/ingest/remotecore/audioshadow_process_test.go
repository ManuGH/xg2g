// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The shadow's adversaries as processes, over a real socket.
//
// The pipe tests beside these are about the conversation. These are about the
// same obligations when the peer is a thing that has to be signalled, waited for
// and reaped - and when getting it wrong leaves something behind on the machine
// rather than a goroutine in the test binary.
//
// Deliberately not the real media core with a flag that makes it hang: a
// production binary should not carry the ability to act broken, and a peer that
// only misbehaves because it was asked to proves less than one built to.

func startHelperShadow(t *testing.T, ctx context.Context, mode string) *RemoteAudioShadow {
	t.Helper()
	requireOwnableCore(t)
	peer, err := startHelperAnywhere(t, ctx, mode)
	if err != nil {
		t.Fatalf("starting a %s peer: %v", mode, err)
	}
	s := &RemoteAudioShadow{peer: peer}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// observeAdversary is the shape all of these share: ask, misbehave, and see
// whether the call still comes back on this side's own terms.
func observeAdversary(t *testing.T, mode string, batches []mediafacts.AudioShadowBatch, cancelAfter time.Duration) error {
	t.Helper()
	s := startHelperShadow(t, context.Background(), mode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.ObserveAudio(ctx, batches)
		done <- err
	}()

	if cancelAfter > 0 {
		time.Sleep(cancelAfter)
		cancel()
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("%s: ObserveAudio succeeded", mode)
		}
		assertRetired(t, s)
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: ObserveAudio never returned; the peer decided how long this side waits", mode)
		return nil
	}
}

// A. The peer takes the request and never answers.
func TestShadowProcess_APeerThatNeverAnswers(t *testing.T) {
	err := observeAdversary(t, "observe-then-say-nothing", oneBatch(), 100*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the caller's own cancellation", err)
	}
}

// B. The peer never reads the request, so this side is stuck in a write. The
// request is larger than any socket buffer will absorb, which is what makes it a
// write that blocks rather than one that quietly succeeds.
func TestShadowProcess_APeerThatStopsReading(t *testing.T) {
	big := []mediafacts.AudioShadowBatch{{PID: 1, Epoch: 1, Feeds: [][]byte{make([]byte, 4<<20)}}}
	err := observeAdversary(t, "observe-then-stop-reading", big, 300*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the caller's own cancellation", err)
	}
}

// C. The peer sends a length prefix and then stops.
func TestShadowProcess_APeerThatSendsHalfAnAnswer(t *testing.T) {
	err := observeAdversary(t, "observe-then-half-an-answer", oneBatch(), 300*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the caller's own cancellation", err)
	}
}

// D. The peer hangs up. No cancellation needed: the socket ending is the answer.
func TestShadowProcess_APeerThatHangsUp(t *testing.T) {
	err := observeAdversary(t, "observe-then-hang-up", oneBatch(), 0)
	if !errors.Is(err, mediafacts.ErrCoreGone) {
		t.Errorf("err = %v, want ErrCoreGone", err)
	}
}

// E. The peer answers with something that is not an answer.
func TestShadowProcess_APeerThatAnswersNonsense(t *testing.T) {
	err := observeAdversary(t, "observe-then-nonsense", oneBatch(), 0)
	if !errors.Is(err, mediafacts.ErrCoreInvalidResponse) {
		t.Errorf("err = %v, want ErrCoreInvalidResponse", err)
	}
}

// Closing a shadow whose peer is inside a call it will never finish. Bounded,
// and the process is gone afterwards - not left running, not left defunct.
func TestShadowProcess_ClosingAHungShadowIsBoundedAndReaps(t *testing.T) {
	s := startHelperShadow(t, context.Background(), "observe-then-say-nothing")
	pid := s.peer.cmd.Process.Pid

	ctx, cancel := context.WithCancel(context.Background())
	stuck := make(chan struct{})
	go func() {
		defer close(stuck)
		_, _ = s.ObserveAudio(ctx, oneBatch())
	}()
	// Long enough that the request is with the peer and this side is blocked on
	// the answer.
	time.Sleep(200 * time.Millisecond)

	began := time.Now()
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if took := time.Since(began); took > termGrace+2*time.Second {
		t.Errorf("Close took %v against a peer that never answers", took)
	}
	cancel()
	<-stuck

	assertReaped(t, pid)
	assertNoZombieChildren(t)
}

// --- the whole point, end to end ------------------------------------------

const (
	e2ePMTPID   = 0x0100
	e2eVideoPID = 0x0200
	e2eAudioPID = 0x012C
)

func e2ePSIPacket(pid uint16, section []byte) []byte {
	pkt := make([]byte, mediafacts.TSPacketSize)
	pkt[0] = mediafacts.SyncByte
	pkt[1] = 0x40 | byte((pid>>8)&0x1F)
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10
	pkt[4] = 0x00
	copy(pkt[5:], section)
	for i := 5 + len(section); i < mediafacts.TSPacketSize; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

func e2ePAT() []byte {
	s := []byte{
		0x00,
		0xB0, 0x0D,
		0x00, 0x01,
		0xC1,
		0x00, 0x00,
		0x00, 0x01,
		0xE0 | byte(e2ePMTPID>>8), byte(e2ePMTPID & 0xFF),
		0, 0, 0, 0,
	}
	binary.BigEndian.PutUint32(s[len(s)-4:], mediafacts.CalculateMPEG2CRC32(s[:len(s)-4]))
	return e2ePSIPacket(0, s)
}

func e2ePMT() []byte {
	descriptors := []byte{0x6A, 0x02, 0x80, 0x04}
	es := []byte{
		0x1B,
		0xE0 | byte(e2eVideoPID>>8), byte(e2eVideoPID & 0xFF),
		0xF0, 0x00,
		0x06,
		0xE0 | byte(e2eAudioPID>>8), byte(e2eAudioPID & 0xFF),
		0xF0 | byte(len(descriptors)>>8), byte(len(descriptors) & 0xFF),
	}
	es = append(es, descriptors...)

	sectionLen := 9 + len(es) + 4
	s := []byte{
		0x02,
		0xB0 | byte(sectionLen>>8), byte(sectionLen & 0xFF),
		0x00, 0x01,
		0xC1,
		0x00, 0x00,
		0xE0 | byte(e2eVideoPID>>8), byte(e2eVideoPID & 0xFF),
		0xF0, 0x00,
	}
	s = append(s, es...)
	s = append(s, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(s[len(s)-4:], mediafacts.CalculateMPEG2CRC32(s[:len(s)-4]))
	return e2ePSIPacket(e2ePMTPID, s)
}

func e2eAudioPackets(es []byte) []byte {
	var out []byte
	first := true
	for len(es) > 0 {
		pkt := make([]byte, mediafacts.TSPacketSize)
		pkt[0] = mediafacts.SyncByte
		pkt[1] = byte((e2eAudioPID >> 8) & 0x1F)
		pkt[2] = byte(e2eAudioPID & 0xFF)
		pkt[3] = 0x10

		body := 4
		if first {
			pkt[1] |= 0x40
			copy(pkt[4:], []byte{0x00, 0x00, 0x01, 0xBD, 0x00, 0x00, 0x80, 0x00, 0x00})
			body = 13
			first = false
		}
		n := copy(pkt[body:], es)
		es = es[n:]
		for i := body + n; i < mediafacts.TSPacketSize; i++ {
			pkt[i] = 0xFF
		}
		out = append(out, pkt...)
	}
	return out
}

// e2eChunk is a small transport stream that names an AC-3 track and then carries
// five syncframes of it.
func e2eChunk() []byte {
	var ac3 []byte
	for i := 0; i < 5; i++ {
		f := make([]byte, 128)
		f[0], f[1] = 0x0B, 0x77
		f[4] = 0x00
		f[5] = 8 << 3
		f[6] = 0x40 // stereo
		ac3 = append(ac3, f...)
	}
	chunk := append([]byte(nil), e2ePAT()...)
	chunk = append(chunk, e2ePMT()...)
	return append(chunk, e2eAudioPackets(ac3)...)
}

// A real shadow process, held inside ObserveAudio, attached to a real core.
//
// This is what 4b.2a and 4b.2b are for, in one test. The peer never answers, so
// the comparison cannot make progress at all; the queue behind it fills, the
// runner retires it, and the only thing that may be visibly different about the
// core is that the comparison stopped. The facts, the offsets and the time it
// takes to produce them belong to Go and to nobody else.
func TestShadowProcess_AHungPeerCannotReachTheAuthoritativePath(t *testing.T) {
	requireOwnableCore(t)

	const chunks = 12
	chunk := e2eChunk()

	// What the core says with nothing watching it. The comparison is a second
	// reader of the same bytes, so this is the answer the shadowed core has to
	// produce as well - not approximately, and not eventually.
	plain := mediafacts.NewGoCore(1)
	var wantResults []mediafacts.ParseResult
	for i := 0; i < chunks; i++ {
		res, err := plain.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk)
		if err != nil {
			t.Fatalf("unshadowed Ingest %d: %v", i, err)
		}
		wantResults = append(wantResults, res)
	}
	wantFacts := plain.Snapshot()

	shadow := startHelperShadow(t, context.Background(), "observe-then-say-nothing")
	core := mediafacts.NewGoCore(1)
	core.SetAudioShadow(shadow)
	defer core.CloseAudioShadow()

	var slowest time.Duration
	for i := 0; i < chunks; i++ {
		began := time.Now()
		res, err := core.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk)
		if took := time.Since(began); took > slowest {
			slowest = took
		}
		if err != nil {
			t.Fatalf("shadowed Ingest %d: %v", i, err)
		}
		if !reflect.DeepEqual(res, wantResults[i]) {
			t.Fatalf("chunk %d: shadowed result %+v, unshadowed %+v", i, res, wantResults[i])
		}
	}

	if got := core.Snapshot(); !reflect.DeepEqual(got, wantFacts) {
		t.Errorf("the facts changed because something was watching:\n got %+v\nwant %+v", got, wantFacts)
	}

	// Nothing here waited for a peer that never answers. A second is generous by
	// three orders of magnitude against a chunk this size, and it is not the
	// number that matters - ten minutes is what waiting would have cost.
	if slowest > time.Second {
		t.Errorf("the slowest Ingest took %v; it waited for the shadow", slowest)
	}

	// And the comparison ended, once, for the reason it should have: the queue
	// filled up behind a peer that never took anything off it.
	report := core.AudioShadowReport()
	if !report.Disabled || report.Errors != 1 {
		t.Errorf("report = %+v, want the comparison retired exactly once", report)
	}
	if report.Compared != 0 {
		t.Errorf("report says %d comparisons happened against a peer that never answered", report.Compared)
	}
	if report.Batches == 0 {
		t.Errorf("report = %+v, want the batches that were accepted before the queue filled", report)
	}
}

// The real core, when there is one. Everything above proves what happens when the
// peer misbehaves; this is the one that proves the two implementations agree when
// it does not.
func TestShadowProcess_TheRealCoreObservesWhatTheGoCoreObserves(t *testing.T) {
	bin := os.Getenv("XG2G_MEDIA_CORE_BIN")
	if bin == "" {
		t.Skip("XG2G_MEDIA_CORE_BIN not set; build media-core and point this at it")
	}
	requireOwnableCore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	shadow, err := StartAudioShadow(ctx, bin)
	if err != nil {
		t.Fatalf("StartAudioShadow: %v", err)
	}
	defer func() {
		if err := shadow.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	core := mediafacts.NewGoCore(1)
	core.SetAudioShadow(shadow)
	defer core.CloseAudioShadow()

	// Paced, one chunk at a time. Ingest never waits for the comparison, so a
	// loop that does not wait either fills the queue in microseconds and retires
	// the shadow on speed alone - which is 4b.2a working exactly as intended, and
	// says nothing at all about whether the two implementations agree. That
	// property has its own test above, with a peer that really is stuck.
	const chunks = 6
	chunk := e2eChunk()
	var report mediafacts.AudioShadowReport
	for i := 0; i < chunks; i++ {
		if _, err := core.Ingest(ctx, int64(i)*int64(len(chunk)), chunk); err != nil {
			t.Fatalf("Ingest %d: %v", i, err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			report = core.AudioShadowReport()
			if report.Compared > uint64(i) || report.Disabled {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the Rust core compared %d of the first %d chunks: %+v", report.Compared, i+1, report)
			}
			time.Sleep(time.Millisecond)
		}
	}

	if report.Disabled {
		t.Fatalf("the comparison was retired: %+v", report)
	}
	if report.Mismatches != 0 {
		t.Fatalf("the two implementations disagreed %d times: %+v", report.Mismatches, report)
	}
	if report.Compared < chunks {
		t.Fatalf("only %d of %d chunks were compared: %+v", report.Compared, chunks, report)
	}
}
