// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

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
	observers map[[2]uint64]*esaudio.Observer
	seen      []AudioShadowBatch
	calls     int

	err     error
	distort func(AudioShadowObservation) AudioShadowObservation
}

func newReplayShadow() *replayShadow {
	return &replayShadow{observers: map[[2]uint64]*esaudio.Observer{}}
}

func (s *replayShadow) ObserveAudio(_ context.Context, batches []AudioShadowBatch) ([]AudioShadowObservation, error) {
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
	return out, nil
}

// batchesFor is every batch the shadow was handed for one stream, in order.
func (s *replayShadow) batchesFor(pid uint16) []AudioShadowBatch {
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

	// Byte for byte and boundary for boundary, not merely "close enough to reach
	// the same answer". A capture one PES header too early reaches the same answer
	// too, and would be a different input the day a parser disagrees about it.
	assertFeeds(t, shadow.batchesFor(shadowAudioPID), wantFeeds)

	want := observedTrack(t, res.Facts, shadowAudioPID)
	if !want.Known() {
		t.Fatalf("the core established nothing to compare against: %+v", want)
	}
	report := core.AudioShadowReport()
	if report.Compared == 0 {
		t.Fatal("nothing was compared")
	}
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

	report := core.AudioShadowReport()
	if report.Mismatches != 0 {
		t.Errorf("mismatches across a reset: %+v", report.Recent)
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

	report := shadowed.AudioShadowReport()
	if !report.Disabled || report.Errors != 1 {
		t.Fatalf("a failed shadow was not retired: %+v", report)
	}

	// A second chunk must not reach it. A shadow that failed once is a second
	// implementation in an unknown state, and reviving it is the transparent
	// restart the process contract already refuses.
	if _, err := shadowed.Ingest(context.Background(), int64(len(chunk)), chunk); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if failing.calls != 1 {
		t.Errorf("the shadow was called %d times after failing once", failing.calls)
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

	report := core.AudioShadowReport()
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
