// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// idrESPayload returns Annex-B elementary stream bytes containing an IDR slice NAL unit.
// Fed to the parser in the clear this is unambiguously indexed as a keyframe, which is what
// makes it a valid probe: if the same bytes are indexed while carried in a scrambled TS
// packet, the parser is treating ciphertext as syntax.
func idrESPayload() []byte {
	return []byte{0x00, 0x00, 0x01, 0x65, 0xAA, 0xBB, 0xCC, 0xDD}
}

// scrambleTSPacket sets transport_scrambling_control to the even key (0b10), exactly as a
// DVB scrambler does when the receiver is not descrambling the service.
func scrambleTSPacket(pkt []byte) []byte {
	pkt[3] = (pkt[3] &^ 0xC0) | 0x80
	return pkt
}

func pushClearPATPMT(t *testing.T, r *MasterRing, pmtPID, videoPID uint16) {
	t.Helper()
	for _, pkt := range createMultiPacketPAT(pmtPID) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PAT failed: %v", err)
		}
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PMT failed: %v", err)
		}
	}
}

// A scrambled video elementary stream must never be parsed as Annex-B, and attach must fail
// with a terminal, self-describing error instead of an indefinite wait for a keyframe that
// cannot arrive. Without the transport_scrambling_control guard the IDR payload below is
// indexed and NewPrimedSubscriber hands out a bogus attach point.
func TestMasterRing_ScrambledVideo_NotIndexedAndAttachIsTerminal(t *testing.T) {
	const pmtPID = 100
	const videoPID = 256

	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushClearPATPMT(t, r, pmtPID, videoPID)

	for i := 0; i < mediafacts.ScrambledVerdictMinPackets; i++ {
		pkt := createVideoPESPacket(videoPID, true, uint8(i&0x0F), idrESPayload())
		if _, err := r.Push(context.Background(), scrambleTSPacket(pkt)); err != nil {
			t.Fatalf("push scrambled video packet %d failed: %v", i, err)
		}
	}

	if offset, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("scrambled payload was indexed as a keyframe at offset %d; ciphertext must never reach the Annex-B parser", offset)
	}

	scrambled, clear := r.ScramblingObservation()
	if scrambled != mediafacts.ScrambledVerdictMinPackets {
		t.Fatalf("expected %d scrambled video packets observed, got %d", mediafacts.ScrambledVerdictMinPackets, scrambled)
	}
	if clear != 0 {
		t.Fatalf("expected zero clear video packets, got %d", clear)
	}

	attach, reader, err := r.NewPrimedSubscriber()
	if err == nil {
		reader.Close()
		t.Fatalf("expected attach to fail on a fully scrambled stream, got attach %+v", attach)
	}
	if !errors.Is(err, ErrScrambledStream) {
		t.Fatalf("expected ErrScrambledStream so the caller can report the receiver is not descrambling, got %v", err)
	}
}

// A single clear payload packet proves the receiver is descrambling, so the scrambled verdict
// must not fire even when scrambled packets dominate. Attach then reports the ordinary
// "no keyframe yet" condition, which the caller is allowed to retry.
func TestMasterRing_ClearPayloadPresent_NotDeclaredScrambled(t *testing.T) {
	const pmtPID = 100
	const videoPID = 256

	r := NewMasterRing(4000 * TSPacketSize)
	defer r.Close()

	pushClearPATPMT(t, r, pmtPID, videoPID)

	for i := 0; i < mediafacts.ScrambledVerdictMinPackets*2; i++ {
		pkt := createVideoPESPacket(videoPID, true, uint8(i&0x0F), idrESPayload())
		if _, err := r.Push(context.Background(), scrambleTSPacket(pkt)); err != nil {
			t.Fatalf("push scrambled video packet %d failed: %v", i, err)
		}
	}

	// A clear non-VCL slice: parsed, but not a random access point.
	clearPkt := createVideoPESPacket(videoPID, true, 0x0F, []byte{0x00, 0x00, 0x01, 0x01, 0x11, 0x22})
	if _, err := r.Push(context.Background(), clearPkt); err != nil {
		t.Fatalf("push clear video packet failed: %v", err)
	}

	scrambled, clear := r.ScramblingObservation()
	if clear != 1 {
		t.Fatalf("expected exactly one clear video packet observed, got %d (scrambled %d)", clear, scrambled)
	}

	_, reader, err := r.NewPrimedSubscriber()
	if err == nil {
		reader.Close()
		t.Fatalf("expected attach to fail without a keyframe")
	}
	if errors.Is(err, ErrScrambledStream) {
		t.Fatalf("stream carries clear payload and must not be declared scrambled, got %v", err)
	}
	if !errors.Is(err, ErrNoKeyframeAvailable) {
		t.Fatalf("expected retryable ErrNoKeyframeAvailable, got %v", err)
	}
}

// Selecting a different program discards everything learned about the previous one, including
// the scrambling observation: the new program may well be free-to-air.
func TestMasterRing_ProgramChange_ResetsScramblingObservation(t *testing.T) {
	const pmtPID = 100
	const videoPID = 256

	r := NewMasterRingWithProgram(4000*TSPacketSize, 1)
	defer r.Close()

	pushClearPATPMT(t, r, pmtPID, videoPID)

	for i := 0; i < mediafacts.ScrambledVerdictMinPackets; i++ {
		pkt := createVideoPESPacket(videoPID, true, uint8(i&0x0F), idrESPayload())
		if _, err := r.Push(context.Background(), scrambleTSPacket(pkt)); err != nil {
			t.Fatalf("push scrambled video packet %d failed: %v", i, err)
		}
	}

	if scrambled, _ := r.ScramblingObservation(); scrambled == 0 {
		t.Fatalf("precondition failed: expected scrambled packets to be observed before program change")
	}

	r.SetTargetProgram(context.Background(), 2)

	scrambled, clear := r.ScramblingObservation()
	if scrambled != 0 || clear != 0 {
		t.Fatalf("expected scrambling observation to reset on program change, got scrambled=%d clear=%d", scrambled, clear)
	}

	_, reader, err := r.NewPrimedSubscriber()
	if err == nil {
		reader.Close()
		t.Fatalf("expected attach to fail after program change")
	}
	if !errors.Is(err, ErrNoKeyframeAvailable) {
		t.Fatalf("expected retryable ErrNoKeyframeAvailable for the freshly selected program, got %v", err)
	}
}
