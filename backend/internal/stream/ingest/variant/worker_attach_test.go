// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
)

// The fixtures below are the minimum needed to drive a MasterRing from the variant
// package: a PAT naming one PMT, a PMT naming one H.264 or HEVC video PID, and a
// video PES carrying an access unit the ring will index as a random access point.
// The ring package has richer builders, but they are unexported test helpers there.

func tsPATPacket(t *testing.T, pmtPID uint16, version uint8) []byte {
	t.Helper()

	section := []byte{
		0x00,       // table_id = PAT
		0xB0, 0x0D, // section_syntax_indicator + length
		0x00, 0x01, // transport_stream_id
		0xC0 | ((version & 0x1F) << 1) | 0x01, // version + current_next
		0x00, 0x00,                            // section_number / last_section_number
		0x00, 0x01, // program_number = 1
		0xE0 | byte((pmtPID>>8)&0x1F), byte(pmtPID & 0xFF),
		0x00, 0x00, 0x00, 0x00, // CRC32
	}
	binary.BigEndian.PutUint32(section[12:16], ring.CalculateMPEG2CRC32(section[:12]))

	pkt := make([]byte, ring.TSPacketSize)
	pkt[0] = ring.SyncByte
	pkt[1] = 0x40 // PUSI, PID 0
	pkt[2] = 0x00
	pkt[3] = 0x10 // payload only
	pkt[4] = 0x00 // pointer_field
	copy(pkt[5:], section)
	return pkt
}

func tsPMTPacket(t *testing.T, pmtPID, videoPID uint16, hevc bool, version uint8) []byte {
	t.Helper()

	streamType := byte(0x1B) // H.264
	if hevc {
		streamType = 0x24 // HEVC
	}

	section := []byte{
		0x02,       // table_id = PMT
		0xB0, 0x17, // section_syntax_indicator + length
		0x00, 0x01, // program_number = 1
		0xC0 | ((version & 0x1F) << 1) | 0x01, // version + current_next
		0x00, 0x00,                            // section_number / last_section_number
		0xE0 | byte((videoPID>>8)&0x1F), byte(videoPID & 0xFF), // PCR PID
		0xF0, 0x00, // program_info_length

		streamType, // video ES
		0xE0 | byte((videoPID>>8)&0x1F), byte(videoPID & 0xFF),
		0xF0, 0x00,

		0x06,       // audio ES (AC-3)
		0xE0, 0x55, // audio PID 85
		0xF0, 0x00,

		0x00, 0x00, 0x00, 0x00, // CRC32
	}
	binary.BigEndian.PutUint32(section[len(section)-4:], ring.CalculateMPEG2CRC32(section[:len(section)-4]))

	pkt := make([]byte, ring.TSPacketSize)
	pkt[0] = ring.SyncByte
	pkt[1] = 0x40 | byte((pmtPID>>8)&0x1F)
	pkt[2] = byte(pmtPID & 0xFF)
	pkt[3] = 0x10
	pkt[4] = 0x00 // pointer_field
	copy(pkt[5:], section)
	return pkt
}

func tsVideoPacket(t *testing.T, videoPID uint16, cc uint8, es []byte) []byte {
	t.Helper()

	pkt := make([]byte, ring.TSPacketSize)
	pkt[0] = ring.SyncByte
	pkt[1] = 0x40 | byte((videoPID>>8)&0x1F) // PUSI
	pkt[2] = byte(videoPID & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F)

	// Minimal PES header with PTS.
	copy(pkt[4:], []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80, 0x80, 0x05,
		0x21, 0x00, 0x01, 0x00, 0x01,
	})
	copy(pkt[18:], es)
	return pkt
}

// h264IDR is an Annex-B start code followed by a NAL header of type 5 (IDR slice).
var h264IDR = []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88}

// hevcIRAP is an Annex-B start code followed by an HEVC IRAP NAL (type 19).
var hevcIRAP = []byte{0x00, 0x00, 0x00, 0x01, 0x26, 0x01}

func newPrimedRing(t *testing.T, capacityPackets int) *ring.MasterRing {
	t.Helper()

	r := ring.NewMasterRing(capacityPackets * ring.TSPacketSize)
	t.Cleanup(r.Close)

	if _, err := r.Push(tsPATPacket(t, 100, 0)); err != nil {
		t.Fatalf("push PAT: %v", err)
	}
	if _, err := r.Push(tsPMTPacket(t, 100, 256, false, 0)); err != nil {
		t.Fatalf("push PMT: %v", err)
	}
	return r
}

// Invariant 2, start case: a worker must begin at a random access point, never at
// the ring tail. The tail is deliberately moved far away from the keyframe here, so
// a tail attach and a keyframe attach cannot be confused.
func TestAttachPrimedMaster_AttachesAtKeyframeNotTail(t *testing.T) {
	r := newPrimedRing(t, 200)

	// Filler ahead of the keyframe, so tail != keyframe offset.
	for i := 0; i < 20; i++ {
		if _, err := r.Push(tsVideoPacket(t, 85, uint8(i), []byte{0xAA})); err != nil {
			t.Fatalf("push filler: %v", err)
		}
	}

	keyframeOffset := r.Head()
	if _, err := r.Push(tsVideoPacket(t, 256, 0, h264IDR)); err != nil {
		t.Fatalf("push keyframe: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := r.Push(tsVideoPacket(t, 256, uint8(i+1), []byte{0xBB})); err != nil {
			t.Fatalf("push trailing video: %v", err)
		}
	}

	attach, reader, err := attachPrimedMaster(context.Background(), r, time.Second)
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	defer func() { _ = reader.Close() }()

	if attach.KeyframeOffset != keyframeOffset {
		t.Fatalf("attached at offset %d, want keyframe offset %d", attach.KeyframeOffset, keyframeOffset)
	}
	if got := reader.Offset(); got != keyframeOffset {
		t.Fatalf("reader positioned at %d, want %d", got, keyframeOffset)
	}
	if attach.KeyframeOffset == r.Tail() {
		t.Fatalf("attach offset equals ring tail %d; the tail is not a decodable entry point", r.Tail())
	}
	if len(attach.Preamble) == 0 {
		t.Fatal("attach carried no PAT/PMT preamble; FFmpeg would receive elementary data without topology")
	}
	if !attach.HasKeyframe {
		t.Fatal("attach reported HasKeyframe=false but returned a reader")
	}
}

// Invariant 2, refusal case: with no random access point in the ring, the old code
// fell back to startOffset -1, which NewSubscriberReader normalises to the tail.
// A refusal is the only correct answer.
func TestAttachPrimedMaster_RefusesTailWhenNoKeyframe(t *testing.T) {
	r := newPrimedRing(t, 200)

	// Audio only: nothing here is a video random access point.
	for i := 0; i < 30; i++ {
		if _, err := r.Push(tsVideoPacket(t, 85, uint8(i), []byte{0xAA})); err != nil {
			t.Fatalf("push audio: %v", err)
		}
	}

	attach, reader, err := attachPrimedMaster(context.Background(), r, 80*time.Millisecond)
	if err == nil {
		_ = reader.Close()
		t.Fatalf("attach succeeded without a keyframe at offset %d; a tail attach must never be returned", attach.KeyframeOffset)
	}
	if reader != nil {
		_ = reader.Close()
		t.Fatal("attach returned a reader alongside an error")
	}
	if !errors.Is(err, ring.ErrNoKeyframeAvailable) {
		t.Fatalf("expected the wait to end on ErrNoKeyframeAvailable, got %v", err)
	}
}

// Invariant 1 and 3: this is the sequence the old three-call attach could not
// survive. It read the preamble, then the keyframe offset, then built the reader,
// and a PMT version bump in between invalidated the index the second call had
// already returned - leaving startOffset -1 and a tail attach carrying the previous
// generation's preamble. The primed attach refuses instead, and only succeeds once
// the new generation has produced its own random access point.
func TestAttachPrimedMaster_PMTChangeInvalidatesRatherThanFallsBackToTail(t *testing.T) {
	r := newPrimedRing(t, 200)

	if _, err := r.Push(tsVideoPacket(t, 256, 0, h264IDR)); err != nil {
		t.Fatalf("push first keyframe: %v", err)
	}

	first, firstReader, err := attachPrimedMaster(context.Background(), r, time.Second)
	if err != nil {
		t.Fatalf("first attach failed: %v", err)
	}
	_ = firstReader.Close()

	// PMT v1 moves video to another PID and codec. The ring invalidates the video
	// state, drops the keyframe index and bumps the generation.
	if _, err := r.Push(tsPMTPacket(t, 100, 512, true, 1)); err != nil {
		t.Fatalf("push PMT v1: %v", err)
	}
	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatal("keyframe index survived the PMT version change; the premise of this test no longer holds")
	}

	// Between generations there is no legal entry point at all.
	_, reader, err := attachPrimedMaster(context.Background(), r, 80*time.Millisecond)
	if err == nil {
		_ = reader.Close()
		t.Fatal("attach succeeded between generations; the only offsets available were stale or the tail")
	}

	// The new generation produces its own random access point.
	newKeyframe := r.Head()
	if _, err := r.Push(tsVideoPacket(t, 512, 0, hevcIRAP)); err != nil {
		t.Fatalf("push second keyframe: %v", err)
	}

	second, secondReader, err := attachPrimedMaster(context.Background(), r, time.Second)
	if err != nil {
		t.Fatalf("attach after new generation keyframe failed: %v", err)
	}
	defer func() { _ = secondReader.Close() }()

	if second.KeyframeOffset != newKeyframe {
		t.Fatalf("attached at %d, want the new generation's keyframe at %d", second.KeyframeOffset, newKeyframe)
	}
	if second.Generation == first.Generation {
		t.Fatalf("generation did not advance across the PMT change (both %d)", first.Generation)
	}
}

// A ring that is closed cannot become primed by waiting. The terminal branch must
// return at once rather than burning the attach timeout. The scrambled counterpart
// of this rule is covered against the ring itself in the ring package.
func TestAttachPrimedMaster_ClosedRingIsTerminalAndDoesNotWait(t *testing.T) {
	r := ring.NewMasterRing(50 * ring.TSPacketSize)
	r.Close()

	start := time.Now()
	_, reader, err := attachPrimedMaster(context.Background(), r, 5*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		_ = reader.Close()
		t.Fatal("attach succeeded on a closed ring")
	}
	if !errors.Is(err, ring.ErrRingClosed) {
		t.Fatalf("expected ErrRingClosed, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("terminal error was retried for %s; it must return immediately", elapsed)
	}
}

// Invariant 1 under churn: whatever the timing, an attach that succeeds must hand
// back one coherent snapshot - a preamble, an offset at or after the tail, and a
// reader standing exactly on that offset. Run with -race this also covers the
// unsynchronised access the three separate calls used to make.
func TestAttachPrimedMaster_ConcurrentGenerationChurnNeverMixesSnapshot(t *testing.T) {
	r := newPrimedRing(t, 400)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Producer: alternate the PMT version and feed each generation a keyframe.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for version := uint8(1); ctx.Err() == nil && version < 30; version++ {
			hevc := version%2 == 1
			videoPID := uint16(256)
			es := h264IDR
			if hevc {
				videoPID = 512
				es = hevcIRAP
			}
			_, _ = r.Push(tsPMTPacket(t, 100, videoPID, hevc, version))
			_, _ = r.Push(tsVideoPacket(t, videoPID, 0, es))
			for i := 0; i < 8; i++ {
				_, _ = r.Push(tsVideoPacket(t, videoPID, uint8(i+1), []byte{0xCC}))
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Consumers: attach repeatedly against the moving ring.
	attaches := 0
	var mu sync.Mutex
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				attach, reader, err := attachPrimedMaster(ctx, r, 50*time.Millisecond)
				if err != nil {
					continue
				}
				// Sample rather than spin: the producer changes generation every
				// couple of milliseconds, so this still crosses many boundaries.
				time.Sleep(time.Millisecond)

				offset := reader.Offset()
				tail := r.Tail()
				_ = reader.Close()

				mu.Lock()
				attaches++
				mu.Unlock()

				if len(attach.Preamble) == 0 {
					t.Errorf("primed attach carried no preamble")
					return
				}
				if attach.KeyframeOffset < tail {
					t.Errorf("attach offset %d is behind the ring tail %d", attach.KeyframeOffset, tail)
					return
				}
				if offset != attach.KeyframeOffset {
					t.Errorf("reader at %d does not match the attach offset %d", offset, attach.KeyframeOffset)
					return
				}
			}
		}()
	}

	wg.Wait()

	if attaches == 0 {
		t.Fatal("no attach succeeded during the churn; the test proved nothing")
	}
	t.Logf("verified %d primed attaches under generation churn", attaches)
}
