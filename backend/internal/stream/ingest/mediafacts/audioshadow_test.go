// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/esaudio"
)

const (
	shadowPMTPID   = 0x0100
	shadowVideoPID = 0x0200
	shadowAudioPID = 0x012C // 300
)

// --- transport fixtures ---------------------------------------------------

func shadowPSIPacket(pid uint16, section []byte) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 | byte((pid>>8)&0x1F) // PUSI
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 // payload only
	pkt[4] = 0x00 // pointer_field
	copy(pkt[5:], section)
	for i := 5 + len(section); i < TSPacketSize; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

func shadowPAT() []byte {
	s := []byte{
		0x00,
		0xB0, 0x0D,
		0x00, 0x01,
		0xC1,
		0x00, 0x00,
		0x00, 0x01,
		0xE0 | byte(shadowPMTPID>>8), byte(shadowPMTPID & 0xFF),
		0, 0, 0, 0,
	}
	binary.BigEndian.PutUint32(s[len(s)-4:], CalculateMPEG2CRC32(s[:len(s)-4]))
	return shadowPSIPacket(0, s)
}

// shadowPMT names one video stream and one AC-3 audio stream on audioPID.
func shadowPMT(version uint8, audioPID uint16) []byte {
	descriptors := []byte{0x6A, 0x02, 0x80, 0x04} // AC-3 descriptor with a component type
	es := []byte{
		0x1B, // H.264
		0xE0 | byte(shadowVideoPID>>8), byte(shadowVideoPID & 0xFF),
		0xF0, 0x00,
		0x06, // private data, made AC-3 by the descriptor
		0xE0 | byte(audioPID>>8), byte(audioPID & 0xFF),
		0xF0 | byte(len(descriptors)>>8), byte(len(descriptors) & 0xFF),
	}
	es = append(es, descriptors...)

	sectionLen := 9 + len(es) + 4
	s := []byte{
		0x02,
		0xB0 | byte(sectionLen>>8), byte(sectionLen & 0xFF),
		0x00, 0x01,
		0xC0 | ((version & 0x1F) << 1) | 0x01,
		0x00, 0x00,
		0xE0 | byte(shadowVideoPID>>8), byte(shadowVideoPID & 0xFF),
		0xF0, 0x00,
	}
	s = append(s, es...)
	s = append(s, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(s[len(s)-4:], CalculateMPEG2CRC32(s[:len(s)-4]))
	return shadowPSIPacket(shadowPMTPID, s)
}

// shadowAudioPackets frames elementary stream bytes the way DVB carries AC-3,
// and returns both the transport packets and the elementary stream slices that
// have to come back out of them.
//
// The expected feeds are taken from how the packets were built, not by parsing
// them again: a test that re-implements the rule it is checking agrees with
// itself for free.
func shadowAudioPackets(pid uint16, es []byte) ([]byte, [][]byte) {
	var out []byte
	var feeds [][]byte
	first := true
	for len(es) > 0 {
		pkt := make([]byte, TSPacketSize)
		pkt[0] = SyncByte
		pkt[1] = byte((pid >> 8) & 0x1F)
		pkt[2] = byte(pid & 0xFF)
		pkt[3] = 0x10

		body := 4
		if first {
			pkt[1] |= 0x40
			copy(pkt[4:], []byte{
				0x00, 0x00, 0x01, 0xBD, // private_stream_1
				0x00, 0x00,
				0x80, 0x00, 0x00,
			})
			body = 13
			first = false
		}
		n := copy(pkt[body:], es)
		es = es[n:]
		for i := body + n; i < TSPacketSize; i++ {
			pkt[i] = 0xFF
		}
		out = append(out, pkt...)
		// Everything after the transport header, and after the PES header where
		// there is one - padding included, because the core does not trim it
		// either.
		feeds = append(feeds, append([]byte(nil), pkt[body:]...))
	}
	return out, feeds
}

func shadowAC3Run(byte6 byte, frames int) []byte {
	var out []byte
	for i := 0; i < frames; i++ {
		f := make([]byte, 128)
		f[0], f[1] = 0x0B, 0x77
		f[4] = 0x00 // 48 kHz, smallest frame
		f[5] = 8 << 3
		f[6] = byte6
		out = append(out, f...)
	}
	return out
}

const (
	shadowByte6Stereo   = 0x40
	shadowByte6Surround = 0xEB
)

// --- the shadow under test ------------------------------------------------

// replayShadow is what every real shadow will be: an observer per stream and
// epoch, fed the batches in the order they were handed over. It keeps a copy of
// the bytes on purpose - the batches point into the caller's chunk and are not
// valid after the call, which is exactly the contract a wire implementation has
// to respect too.
type replayShadow struct {
	mu        sync.Mutex
	observers map[[2]uint64]*esaudio.Observer
	seen      []AudioShadowBatch
	calls     int

	// entries counts calls that have been reached, as opposed to calls that have
	// answered. A shadow held in the block below has been reached and has not
	// answered, and which of the two a test waits for is the difference between
	// watching the worker and watching the comparison.
	entries atomic.Int64

	// block, when set, holds ObserveAudio until it is closed. The worker is the
	// only thing waiting on it, which is the property most of these tests are
	// about.
	block chan struct{}

	err     error
	distort func(AudioShadowObservation) AudioShadowObservation
	// mangle rewrites the whole answer, for the cases about a shadow that has
	// lost track of what it was asked.
	mangle func([]AudioShadowObservation) []AudioShadowObservation
}

func newReplayShadow() *replayShadow {
	return &replayShadow{observers: map[[2]uint64]*esaudio.Observer{}}
}

func (s *replayShadow) ObserveAudio(ctx context.Context, batches []AudioShadowBatch) ([]AudioShadowObservation, error) {
	s.entries.Add(1)
	// Held here until the test lets go - or until the context is cancelled, which
	// is the AudioShadow contract and the only reason anything downstream of this
	// can promise that a worker ends.
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]AudioShadowObservation, 0, len(batches))
	for _, b := range batches {
		copied := AudioShadowBatch{PID: b.PID, Epoch: b.Epoch}
		for _, f := range b.Feeds {
			copied.Feeds = append(copied.Feeds, append([]byte(nil), f...))
		}
		s.seen = append(s.seen, copied)

		key := [2]uint64{uint64(b.PID), b.Epoch}
		o := s.observers[key]
		if o == nil {
			o = esaudio.NewObserver()
			s.observers[key] = o
		}
		for _, f := range copied.Feeds {
			o.Feed(f)
		}
		obs := AudioShadowObservation{PID: b.PID, Epoch: b.Epoch, Observation: o.Current()}
		if s.distort != nil {
			obs = s.distort(obs)
		}
		out = append(out, obs)
	}
	if s.mangle != nil {
		out = s.mangle(out)
	}
	return out, nil
}

// batchesFor is every batch the shadow was handed for one stream, in order.
func (s *replayShadow) batchesFor(pid uint16) []AudioShadowBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AudioShadowBatch
	for _, b := range s.seen {
		if b.PID == pid {
			out = append(out, b)
		}
	}
	return out
}

// assertFeeds requires the captured feeds to be exactly the slices the packets
// were built from, in order.
func assertFeeds(t *testing.T, batches []AudioShadowBatch, want [][]byte) {
	t.Helper()
	var got [][]byte
	for _, b := range batches {
		got = append(got, b.Feeds...)
	}
	if len(got) != len(want) {
		t.Fatalf("captured %d feeds, want %d - the capture is not at the same call the observer is fed at",
			len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("feed %d differs (%d bytes captured, %d expected)\n captured %x…\n expected %x…",
				i, len(got[i]), len(want[i]), got[i][:min(12, len(got[i]))], want[i][:min(12, len(want[i]))])
		}
	}
}

func (s *replayShadow) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// enteredCount is how many calls have been reached, answered or not.
func (s *replayShadow) enteredCount() int {
	return int(s.entries.Load())
}

// awaitEntered waits for the worker to be inside the shadow.
func awaitEntered(t *testing.T, s *replayShadow, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.enteredCount() < want {
		if time.Now().After(deadline) {
			t.Fatalf("the worker reached the shadow %d times, want %d", s.enteredCount(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// awaitShadow waits for the worker to get where the test needs it.
//
// The comparison is off the ingest path now, so "after Ingest returned" is no
// longer "after the shadow answered". Polling a counter is what that costs.
func awaitShadow(t *testing.T, core *GoCore, what string, done func(AudioShadowReport) bool) AudioShadowReport {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		report := core.AudioShadowReport()
		if done(report) {
			return report
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; report = %+v", what, report)
		}
		time.Sleep(time.Millisecond)
	}
}

func observedTrack(t *testing.T, facts Facts, pid uint16) esaudio.Observation {
	t.Helper()
	for _, tr := range facts.AudioTracks {
		if tr.PID == pid {
			return tr.Observed
		}
	}
	t.Fatalf("no audio track %d in %+v", pid, facts.AudioTracks)
	return esaudio.Observation{}
}

// --- tests ----------------------------------------------------------------

// The seam's whole claim: the shadow is fed what the observer was fed. Replaying
// the captured feeds has to reproduce the core's own observation exactly - not
// approximately, and not only the layout.
func TestAudioShadow_ReplayingTheCapturedFeedsReproducesTheCoresOwnObservation(t *testing.T) {
	core := NewGoCore(1)
	shadow := newReplayShadow()
	core.SetAudioShadow(shadow)

	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, wantFeeds := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk = append(chunk, audio...)

	res, err := core.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	want := observedTrack(t, res.Facts, shadowAudioPID)
	if !want.Known() {
		t.Fatalf("the core established nothing to compare against: %+v", want)
	}
	report := awaitShadow(t, core, "the first comparison", func(r AudioShadowReport) bool {
		return r.Compared > 0 || r.Disabled
	})
	// Byte for byte and boundary for boundary, not merely "close enough to reach
	// the same answer". A capture one PES header too early reaches the same answer
	// too, and would be a different input the day a parser disagrees about it.
	assertFeeds(t, shadow.batchesFor(shadowAudioPID), wantFeeds)
	if report.Mismatches != 0 {
		t.Fatalf("the replayed feeds disagreed with the core: %+v", report.Recent)
	}
	if report.Disabled {
		t.Fatal("the shadow was disabled without failing")
	}
}

// The adversary that matters: the reset happens inside one Ingest call, not
// between two of them. Both halves of the chunk are the same PID, and only the
// epoch says they are not the same stream.
func TestAudioShadow_AnEpochChangeInsideOneChunkSplitsTheBatches(t *testing.T) {
	core := NewGoCore(1)
	shadow := newReplayShadow()
	core.SetAudioShadow(shadow)

	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	stereo, stereoFeeds := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Stereo, 4))
	chunk = append(chunk, stereo...)
	// A new PMT version for the same PID: a different elementary stream that
	// happens to share a number.
	chunk = append(chunk, shadowPMT(1, shadowAudioPID)...)
	surround, surroundFeeds := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 4))
	chunk = append(chunk, surround...)

	res, err := core.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	awaitShadow(t, core, "both epochs to be compared", func(r AudioShadowReport) bool {
		return r.Compared >= 2 || r.Disabled
	})
	batches := shadow.batchesFor(shadowAudioPID)
	if len(batches) != 2 {
		t.Fatalf("got %d batches for PID %d, want 2 - the feeds either side of the reset are not one stream",
			len(batches), shadowAudioPID)
	}
	if batches[0].Epoch == batches[1].Epoch {
		t.Fatalf("both batches carry epoch %d; the reset did not turn it", batches[0].Epoch)
	}
	if batches[1].Epoch < batches[0].Epoch {
		t.Fatalf("the epoch went backwards: %d then %d", batches[0].Epoch, batches[1].Epoch)
	}
	assertFeeds(t, batches[:1], stereoFeeds)
	assertFeeds(t, batches[1:], surroundFeeds)

	// The second batch is the only one the core still has an observer for, and
	// what it establishes must be the new layout counted from zero.
	after := observedTrack(t, res.Facts, shadowAudioPID)
	if after.Channels != 6 || !after.LFE {
		t.Fatalf("the core did not follow the new stream: %+v", after)
	}
	replayed := esaudio.NewObserver()
	for _, f := range batches[1].Feeds {
		replayed.Feed(f)
	}
	if fields := esaudio.Compare(after, replayed.Current()); len(fields) > 0 {
		t.Errorf("replaying the post-reset batch disagrees about %v:\n core %+v\n replay %+v",
			fields, after, replayed.Current())
	}
	// And the pre-reset frames are not in it. Folding the two runs together would
	// hand a shadow four frames of a layout this stream never had.
	if replayed.Current().Frames != 4 {
		t.Errorf("the post-reset batch carries %d frames, want 4 - it contains bytes from before the reset",
			replayed.Current().Frames)
	}

	report := awaitShadow(t, core, "both epochs to be compared", func(r AudioShadowReport) bool {
		return r.Compared >= 2 || r.Disabled
	})
	if report.Batches != 2 || report.Compared != 2 {
		t.Fatalf("report = %+v; both epochs are handed over and both have a reference to be held against - "+
			"comparing only the one whose observer is still alive is how a shadow that gets the ended epoch "+
			"wrong reports no disagreement at all", report)
	}
	if report.Mismatches != 0 {
		t.Errorf("mismatches across a reset: %+v", report.Recent)
	}
}

// The reference for an epoch that ended inside this chunk has to outlive the
// observer that produced it. A shadow that is wrong only about the stream that
// has already gone is exactly the PID-reuse bug this differential exists for.
func TestAudioShadow_ADisagreementAboutTheEndedEpochIsStillFound(t *testing.T) {
	core := NewGoCore(1)
	shadow := newReplayShadow()
	core.SetAudioShadow(shadow)

	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	stereo, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Stereo, 4))
	chunk = append(chunk, stereo...)
	chunk = append(chunk, shadowPMT(1, shadowAudioPID)...)
	surround, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 4))
	chunk = append(chunk, surround...)

	// Wrong about the first epoch only, and right about the one the core still
	// has an observer for.
	var firstEpoch uint64
	shadow.distort = func(o AudioShadowObservation) AudioShadowObservation {
		if firstEpoch == 0 || o.Epoch < firstEpoch {
			firstEpoch = o.Epoch
		}
		if o.Epoch == firstEpoch {
			o.Observation.Channels = 6
			o.Observation.LFE = true
		}
		return o
	}

	if _, err := core.Ingest(context.Background(), 0, chunk); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	report := awaitShadow(t, core, "both epochs to be compared", func(r AudioShadowReport) bool {
		return r.Compared >= 2 || r.Disabled
	})
	if report.Compared != 2 {
		t.Fatalf("compared %d of 2 batches: %+v", report.Compared, report)
	}
	if report.Mismatches != 1 || len(report.Recent) != 1 {
		t.Fatalf("a disagreement about the ended epoch went unreported: %+v", report)
	}
	if report.Recent[0].Epoch != firstEpoch {
		t.Errorf("mismatch reported on epoch %d, want %d - the ended one", report.Recent[0].Epoch, firstEpoch)
	}
	if !reflect.DeepEqual(report.Recent[0].Fields, []string{"channels", "lfe"}) {
		t.Errorf("mismatch fields = %v, want [channels lfe]", report.Recent[0].Fields)
	}
}

// An answer that does not line up with the question is a shadow that has lost
// track of what it was asked, not a smaller comparison.
func TestAudioShadow_AnAnswerThatDoesNotLineUpRetiresTheShadow(t *testing.T) {
	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk = append(chunk, audio...)

	plain := NewGoCore(1)
	plainResult, err := plain.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("Ingest without a shadow: %v", err)
	}

	cases := []struct {
		name   string
		mangle func([]AudioShadowObservation) []AudioShadowObservation
	}{
		{"nothing at all", func([]AudioShadowObservation) []AudioShadowObservation { return nil }},
		{"one too many", func(o []AudioShadowObservation) []AudioShadowObservation {
			return append(o, o[0])
		}},
		{"another stream's pid", func(o []AudioShadowObservation) []AudioShadowObservation {
			o[0].PID++
			return o
		}},
		{"another epoch", func(o []AudioShadowObservation) []AudioShadowObservation {
			o[0].Epoch++
			return o
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core := NewGoCore(1)
			shadow := newReplayShadow()
			shadow.mangle = tc.mangle
			core.SetAudioShadow(shadow)

			got, err := core.Ingest(context.Background(), 0, chunk)
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if !reflect.DeepEqual(plainResult, got) {
				t.Fatalf("a shadow that lost track changed the authoritative result")
			}
			report := awaitShadow(t, core, "the shadow to be retired", func(r AudioShadowReport) bool {
				return r.Disabled
			})
			if report.Errors != 1 {
				t.Fatalf("the shadow was not retired cleanly: %+v", report)
			}
			if report.Compared != 0 || report.Mismatches != 0 {
				t.Errorf("an answer that does not line up was believed in part: %+v", report)
			}
		})
	}
}

// A shadow is asked, never obeyed. The authoritative result of a chunk has to be
// byte for byte what it would have been with no shadow attached at all.
func TestAudioShadow_AFailingShadowChangesNothingAndIsNotAskedAgain(t *testing.T) {
	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk = append(chunk, audio...)

	plain := NewGoCore(1)
	plainResult, err := plain.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("Ingest without a shadow: %v", err)
	}

	shadowed := NewGoCore(1)
	failing := newReplayShadow()
	failing.err = errors.New("the shadow fell over")
	shadowed.SetAudioShadow(failing)

	shadowedResult, err := shadowed.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("Ingest with a failing shadow: %v", err)
	}
	if !reflect.DeepEqual(plainResult, shadowedResult) {
		t.Fatalf("a failing shadow changed the authoritative result:\n plain %+v\nshadowed %+v",
			plainResult, shadowedResult)
	}

	report := awaitShadow(t, shadowed, "the shadow to be retired", func(r AudioShadowReport) bool {
		return r.Disabled
	})
	if report.Errors != 1 {
		t.Fatalf("a failed shadow was not retired cleanly: %+v", report)
	}

	// A second chunk must not reach it. A shadow that failed once is a second
	// implementation in an unknown state, and reviving it is the transparent
	// restart the process contract already refuses.
	if _, err := shadowed.Ingest(context.Background(), int64(len(chunk)), chunk); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if failing.callCount() != 1 {
		t.Errorf("the shadow was called %d times after failing once", failing.callCount())
	}
}

// A disagreement is recorded with the fields it is about, and changes nothing.
func TestAudioShadow_AMismatchNamesTheFieldsAndLeavesTheFactsAlone(t *testing.T) {
	core := NewGoCore(1)
	shadow := newReplayShadow()
	shadow.distort = func(o AudioShadowObservation) AudioShadowObservation {
		o.Observation.Channels = 2
		o.Observation.Frames++
		return o
	}
	core.SetAudioShadow(shadow)

	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk = append(chunk, audio...)

	res, err := core.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if got := observedTrack(t, res.Facts, shadowAudioPID); got.Channels != 6 {
		t.Fatalf("the shadow's opinion reached the facts: %+v", got)
	}

	report := awaitShadow(t, core, "the mismatch to be recorded", func(r AudioShadowReport) bool {
		return r.Compared > 0 || r.Disabled
	})
	if report.Mismatches != 1 || len(report.Recent) != 1 {
		t.Fatalf("mismatch not recorded: %+v", report)
	}
	if got := report.Recent[0].Fields; !reflect.DeepEqual(got, []string{"channels", "frames"}) {
		t.Errorf("mismatch fields = %v, want [channels frames]", got)
	}
	if report.Disabled {
		t.Error("a disagreement retired the shadow; only a failure does that")
	}
}

// Without a shadow the core does not even collect the feeds.
func TestAudioShadow_NothingIsCapturedWithoutOne(t *testing.T) {
	core := NewGoCore(1)
	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk = append(chunk, audio...)

	if _, err := core.Ingest(context.Background(), 0, chunk); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(core.shadowBatches) != 0 {
		t.Errorf("captured %d batches with no shadow attached", len(core.shadowBatches))
	}
	if got := core.AudioShadowReport(); got.Batches != 0 || got.Compared != 0 {
		t.Errorf("a core without a shadow reported %+v", got)
	}
}

// --- the captured feeds' lifetime ------------------------------------------

// assertNoCapturedFeeds looks past the length into the backing array. Reslicing
// to zero hides the elements without releasing what they point at, and what they
// point at is the chunk that has just been interpreted.
func assertNoCapturedFeeds(t *testing.T, core *GoCore) {
	t.Helper()
	if len(core.shadowBatches) != 0 {
		t.Errorf("%d batches left after Ingest returned", len(core.shadowBatches))
	}
	full := core.shadowBatches[:cap(core.shadowBatches)]
	for i, b := range full {
		if b.Batch.Feeds != nil {
			t.Errorf("slot %d of the batch array still holds %d feed slices into a chunk that is gone",
				i, len(b.Batch.Feeds))
		}
		if b.Batch.PID != 0 || b.Batch.Epoch != 0 {
			t.Errorf("slot %d still describes stream %d epoch %d", i, b.Batch.PID, b.Batch.Epoch)
		}
	}
}

// stallingContext is fine for the first few checks and cancelled after them, so
// a chunk can be given up on at a known point rather than a hoped-for one.
type stallingContext struct {
	context.Context
	okChecks int
	checks   int
}

func (s *stallingContext) Err() error {
	s.checks++
	if s.checks <= s.okChecks {
		return nil
	}
	return context.Canceled
}

func TestAudioShadow_TheCapturedFeedsDoNotOutliveTheChunk(t *testing.T) {
	core := NewGoCore(1)
	core.SetAudioShadow(newReplayShadow())

	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk = append(chunk, audio...)

	if _, err := core.Ingest(context.Background(), 0, chunk); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	assertNoCapturedFeeds(t, core)
}

// The path that never reaches the shadow at all. Audio is captured, the caller
// gives up part way through the chunk, and Ingest returns an error - and the
// feeds still have to be gone, because the chunk they point into is.
func TestAudioShadow_AnAbandonedChunkLeavesNoFeedsBehind(t *testing.T) {
	core := NewGoCore(1)
	core.SetAudioShadow(newReplayShadow())

	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk = append(chunk, audio...)
	// Past the next context check, so the chunk is abandoned after the audio has
	// been captured rather than before.
	null := make([]byte, TSPacketSize)
	null[0], null[1], null[2], null[3] = SyncByte, 0x1F, 0xFF, 0x10
	for len(chunk) < (ctxCheckPackets+8)*TSPacketSize {
		chunk = append(chunk, null...)
	}

	// One check at the top of Ingest, one at packet 0, and then the one at packet
	// 4096 is where it gives up.
	ctx := &stallingContext{Context: context.Background(), okChecks: 2}
	if _, err := core.Ingest(ctx, 0, chunk); !errors.Is(err, context.Canceled) {
		t.Fatalf("Ingest err = %v, want context.Canceled", err)
	}
	if core.AudioShadowReport().Batches != 0 {
		t.Fatal("the shadow was run for a chunk the caller gave up on")
	}
	assertNoCapturedFeeds(t, core)
}

// --- the shadow is not on the authoritative path ---------------------------

// shadowChunk is what these tests hand over: a program, one AC-3 stream, and
// enough frames for there to be an observation to disagree about.
func shadowChunk() []byte {
	chunk := append([]byte(nil), shadowPAT()...)
	chunk = append(chunk, shadowPMT(0, shadowAudioPID)...)
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	return append(chunk, audio...)
}

// withinDeadline runs f and fails the test if it has not come back in time.
//
// A blocked hand-off would not be a slow test, it would be a hung one, and a
// hung test says "timeout after 10 minutes" without saying where. This says
// where.
func withinDeadline(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not come back within %s", what, d)
	}
}

// The invariant the whole step is for: a shadow that never answers cannot slow
// down, fail or change a single authoritative ingest.
//
// Both cores are given the same chunks in the same order and their results are
// held against each other on every one of them - not just at the end, because a
// shadow that perturbed the middle of the run and was right again by the end
// would pass that.
func TestAudioShadow_AHangingShadowNeverHoldsUpIngest(t *testing.T) {
	const chunks = 100
	chunk := shadowChunk()

	plain := NewGoCore(1)
	shadowed := NewGoCore(1)
	hanging := newReplayShadow()
	hanging.block = make(chan struct{})
	shadowed.SetAudioShadow(hanging)
	defer shadowed.CloseAudioShadow()
	defer close(hanging.block)

	results := make([]ParseResult, 0, chunks)
	first, err := shadowed.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("shadowed Ingest 0: %v", err)
	}
	results = append(results, first)
	// Waited for on purpose. The interesting run is the one where the worker is
	// already inside a call that will never return, not the one where the
	// scheduler never got round to starting it.
	awaitEntered(t, hanging, 1)

	withinDeadline(t, 30*time.Second, fmt.Sprintf("%d ingests behind a shadow that never answers", chunks-1), func() {
		for i := 1; i < chunks; i++ {
			got, err := shadowed.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk)
			if err != nil {
				t.Errorf("shadowed Ingest %d: %v", i, err)
				return
			}
			results = append(results, got)
		}
	})
	if t.Failed() {
		return
	}

	for i := 0; i < chunks; i++ {
		want, err := plain.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk)
		if err != nil {
			t.Fatalf("plain Ingest %d: %v", i, err)
		}
		if !reflect.DeepEqual(want, results[i]) {
			t.Fatalf("chunk %d: a shadow that never answered changed the authoritative result", i)
		}
	}

	// The shadow was reached once and never answered: the test never let go of it,
	// and what ended the call in the end was the retirement cancelling it. What
	// kept the ingests going was the hand-off never waiting - not the shadow
	// quietly finishing, and not the queue quietly draining.
	if got, answered := hanging.enteredCount(), hanging.callCount(); got != 1 || answered != 0 {
		t.Errorf("the shadow was reached %d times and answered %d; the test never let the first call return",
			got, answered)
	}
}

// A queue that fills up ends the comparison. It does not drop a chunk and carry
// on: the shadow's observers are stateful, so the batch after a missing one is a
// comparison against a stream it never saw.
func TestAudioShadow_AFullQueueRetiresRatherThanSkippingAChunk(t *testing.T) {
	chunk := shadowChunk()

	core := NewGoCore(1)
	blocked := newReplayShadow()
	blocked.block = make(chan struct{})
	core.SetAudioShadow(blocked)
	runner := core.shadowRunner
	defer core.CloseAudioShadow()

	plain := NewGoCore(1)

	ingest := func(i int) {
		t.Helper()
		want, err := plain.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk)
		if err != nil {
			t.Fatalf("plain Ingest %d: %v", i, err)
		}
		var got ParseResult
		withinDeadline(t, 10*time.Second, fmt.Sprintf("Ingest %d", i), func() {
			var err error
			got, err = core.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk)
			if err != nil {
				t.Errorf("Ingest %d: %v", i, err)
			}
		})
		if t.Failed() {
			t.FailNow()
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("chunk %d: the result differs from a core with no shadow attached", i)
		}
	}

	// The first one leaves the queue and is stuck inside the shadow. Waiting for
	// that is what makes the count below the queue's and not a race with it.
	ingest(0)
	awaitEntered(t, blocked, 1)

	// Now fill the queue exactly, and then hand over one more.
	for i := 1; i <= audioShadowQueueDepth; i++ {
		ingest(i)
		if report := core.AudioShadowReport(); report.Disabled {
			t.Fatalf("retired at chunk %d, before the queue was full: %+v", i, report)
		}
	}
	ingest(audioShadowQueueDepth + 1)

	report := core.AudioShadowReport()
	if !report.Disabled || report.Errors != 1 {
		t.Fatalf("a full queue did not retire the shadow exactly once: %+v", report)
	}
	// One batch per chunk, and only the ones that were taken.
	if report.Batches != uint64(audioShadowQueueDepth+1) {
		t.Errorf("Batches = %d, want %d - the chunk that found the queue full is not an accepted batch",
			report.Batches, audioShadowQueueDepth+1)
	}

	// And the retirement reaches the call that is already in progress. Nobody
	// closes the block and nobody calls Close: the cancellation the retirement
	// carries is the whole reason the worker can end here at all.
	select {
	case <-runner.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker outlived the retirement; a retirement decided on the producer side never reached it")
	}
	if got := blocked.enteredCount(); got != 1 {
		t.Errorf("the shadow was reached %d times; everything queued behind the retirement was carried on with", got)
	}
	// What was still queued is abandoned rather than caught up on. It is a run
	// with a hole in front of it, and comparing across that hole is the thing this
	// design refuses to do.
	if len(runner.queue) == 0 {
		t.Error("the queue was drained after the retirement; those batches were compared or dropped, and either would be a lie")
	}
	close(blocked.block)

	// And a chunk after all that reaches nothing at all.
	ingest(audioShadowQueueDepth + 2)
	if got := core.AudioShadowReport(); got.Errors != 1 || got.Batches != uint64(audioShadowQueueDepth+1) {
		t.Errorf("work was accepted after the retirement: %+v", got)
	}
}

// The hand-off owns its bytes. The chunk is the caller's and is gone the moment
// Ingest returns, so the copy is made before that - and this is the test that
// the copy is a copy.
func TestAudioShadow_TheWorkOwnsItsBytesOnceIngestHasReturned(t *testing.T) {
	head := append([]byte(nil), shadowPAT()...)
	head = append(head, shadowPMT(0, shadowAudioPID)...)
	audio, wantFeeds := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, 5))
	chunk := append(head, audio...)

	core := NewGoCore(1)
	// Blocked, so the shadow is guaranteed to still be waiting when the chunk it
	// was given is overwritten underneath it.
	shadow := newReplayShadow()
	shadow.block = make(chan struct{})
	core.SetAudioShadow(shadow)
	defer core.CloseAudioShadow()

	res, err := core.Ingest(context.Background(), 0, chunk)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	want := observedTrack(t, res.Facts, shadowAudioPID)
	if !want.Known() {
		t.Fatalf("the core established nothing to compare against: %+v", want)
	}

	// The caller reuses its buffer, which is what a caller with a staging buffer
	// does between chunks.
	for i := range chunk {
		chunk[i] = 0xAA
	}
	close(shadow.block)

	report := awaitShadow(t, core, "the comparison to happen after the chunk was overwritten", func(r AudioShadowReport) bool {
		return r.Compared > 0 || r.Disabled
	})
	if report.Disabled {
		t.Fatalf("the shadow was retired: %+v", report)
	}
	assertFeeds(t, shadow.batchesFor(shadowAudioPID), wantFeeds)
	if report.Mismatches != 0 {
		t.Fatalf("the shadow read the overwritten chunk: %+v", report.Recent)
	}
}

// Close cancels whatever the worker is inside and does not come back until the
// goroutine is gone.
//
// There is no companion case for a shadow that ignores its context, because
// there is nothing to assert about one. Go cannot end a goroutine parked in an
// interface method from the outside, so bounded shutdown and an accounted-for
// worker cannot both be had against arbitrary code - which is why cancellation
// is a contract in the AudioShadow doc rather than a hope in this file. Proving
// a real implementation keeps it against a peer that never replies belongs to
// the RemoteAudioShadow in 4b.2b, and cannot be proven here.
func TestAudioShadow_CloseCancelsTheShadowAndWaitsForTheWorker(t *testing.T) {
	chunk := shadowChunk()
	core := NewGoCore(1)
	stuck := newReplayShadow()
	stuck.block = make(chan struct{})
	defer close(stuck.block)
	core.SetAudioShadow(stuck)
	runner := core.shadowRunner

	if _, err := core.Ingest(context.Background(), 0, chunk); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	awaitEntered(t, stuck, 1)

	// Nothing else can end that call: the block is still shut, and only the
	// cancellation Close carries can get the shadow out of it.
	withinDeadline(t, 5*time.Second, "CloseAudioShadow with the shadow still waiting", core.CloseAudioShadow)

	// Not "eventually, probably". Close having returned is the goroutine having
	// ended, or the wait in it was decoration.
	select {
	case <-runner.done:
	default:
		t.Fatal("Close returned with the worker still running")
	}

	// A shutdown is not a failure. A report that called it one would say the
	// comparison stopped for a reason that never happened.
	if got := runner.snapshot(); got.Errors != 0 {
		t.Errorf("a clean Close was reported as %d errors: %+v", got.Errors, got)
	}
}

// Reset discards the stream, not the comparison. The worker is nobody else's to
// stop, and the epoch is the shadow's key for per-stream state - restarting it
// at zero would hand a stateful shadow a stream it thinks it already knows.
func TestAudioShadow_ResetKeepsTheShadowAndNeverReusesAnEpoch(t *testing.T) {
	chunk := shadowChunk()
	core := NewGoCore(1)
	shadow := newReplayShadow()
	core.SetAudioShadow(shadow)
	defer core.CloseAudioShadow()

	if _, err := core.Ingest(context.Background(), 0, chunk); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	awaitShadow(t, core, "the first chunk to be compared", func(r AudioShadowReport) bool {
		return r.Compared > 0 || r.Disabled
	})

	core.Reset()
	if core.shadowRunner == nil {
		t.Fatal("Reset dropped the shadow's worker, which nothing else can stop")
	}

	if _, err := core.Ingest(context.Background(), int64(len(chunk)), chunk); err != nil {
		t.Fatalf("Ingest after Reset: %v", err)
	}
	report := awaitShadow(t, core, "the chunk after the reset to be compared", func(r AudioShadowReport) bool {
		return r.Compared > 1 || r.Disabled
	})
	if report.Disabled {
		t.Fatalf("the shadow was retired across a reset: %+v", report)
	}
	if report.Mismatches != 0 {
		t.Fatalf("mismatches across a reset: %+v", report.Recent)
	}

	batches := shadow.batchesFor(shadowAudioPID)
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want one per chunk", len(batches))
	}
	if batches[1].Epoch <= batches[0].Epoch {
		t.Fatalf("epoch %d then %d: a reset handed the shadow a key it had already used for a stream that is gone",
			batches[0].Epoch, batches[1].Epoch)
	}
}

// --- what the isolation costs the ingest path ------------------------------

// echoShadow answers the question it was asked and does nothing else.
//
// A benchmark of the hand-off wants the cheapest shadow that still counts as
// one: what is being measured is the capture, the copy and the offer, not how
// long somebody else's parser takes.
type echoShadow struct{}

func (echoShadow) ObserveAudio(_ context.Context, batches []AudioShadowBatch) ([]AudioShadowObservation, error) {
	out := make([]AudioShadowObservation, len(batches))
	for i, b := range batches {
		out[i] = AudioShadowObservation{PID: b.PID, Epoch: b.Epoch}
	}
	return out, nil
}

// benchChunk builds about size bytes of transport, audioPercent of it audio and
// the rest null packets.
//
// Null padding rather than video, so that the difference between the two arms is
// the audio path and nothing else. It also makes the baseline cheaper than a
// real chunk, so the relative overhead measured here is an upper bound on what a
// stream with video in it would see.
func benchChunk(size int, audioPercent int) []byte {
	packets := size / TSPacketSize
	audioPackets := packets * audioPercent / 100
	// 184 payload bytes per packet, 128 per AC-3 frame, and the first packet of
	// the run loses 9 of them to the PES header.
	frames := (audioPackets*184 - 9) / 128
	if frames < 1 {
		frames = 1
	}
	audio, _ := shadowAudioPackets(shadowAudioPID, shadowAC3Run(shadowByte6Surround, frames))

	null := make([]byte, TSPacketSize)
	null[0], null[1], null[2], null[3] = SyncByte, 0x1F, 0xFF, 0x10

	out := append([]byte(nil), shadowPAT()...)
	out = append(out, shadowPMT(0, shadowAudioPID)...)
	sent := 0
	for i := 0; i < packets; i++ {
		if sent*TSPacketSize < len(audio) && i*audioPercent/100 >= sent {
			out = append(out, audio[sent*TSPacketSize:(sent+1)*TSPacketSize]...)
			sent++
			continue
		}
		out = append(out, null...)
	}
	return out
}

// BenchmarkIngestAudioShadow is the price of the isolation.
//
// Not a threshold and deliberately not tied to any deadline: what is bounded by
// design is that the shadow's own speed is not in here at all. What is left is a
// copy of the chunk's audio and a channel send, and this is how big that is.
func BenchmarkIngestAudioShadow(b *testing.B) {
	const chunkSize = 4 << 20
	for _, share := range []int{10, 100} {
		chunk := benchChunk(chunkSize, share)

		b.Run(fmt.Sprintf("audio=%d%%/no-shadow", share), func(b *testing.B) {
			core := NewGoCore(1)
			b.SetBytes(int64(len(chunk)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := core.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("audio=%d%%/async-shadow", share), func(b *testing.B) {
			core := NewGoCore(1)
			core.SetAudioShadow(echoShadow{})
			defer core.CloseAudioShadow()
			b.SetBytes(int64(len(chunk)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := core.Ingest(context.Background(), int64(i)*int64(len(chunk)), chunk); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			// Without this the fast run is the one where the shadow was retired
			// early and the core stopped capturing - which is the unshadowed path
			// wearing the shadowed arm's name.
			if report := core.AudioShadowReport(); report.Disabled {
				b.Fatalf("the shadow was retired during the measurement: %+v", report)
			}
		})
	}
}
