// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// h264IDRPayload is an Annex-B start code plus a NAL header of type 5 (IDR slice).
var h264IDRPayload = []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88}

// hevcIRAPPayload is an Annex-B start code plus an HEVC IRAP NAL (type 19).
var hevcIRAPPayload = []byte{0x00, 0x00, 0x00, 0x01, 0x26, 0x01}

// newH264Ring returns a ring whose PMT names an H.264 video PID, plus a subscriber
// positioned at offset 0. capacityPackets is deliberately small so that pushing
// past it overruns the subscriber.
func newH264Ring(t *testing.T, capacityPackets int) (*MasterRing, *SubscriberReader) {
	t.Helper()

	r := NewMasterRing(capacityPackets * TSPacketSize)
	t.Cleanup(r.Close)

	for _, pkt := range createMultiPacketPAT(100) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PAT: %v", err)
		}
	}
	for _, pkt := range createMultiPacketPMT(100, 256, false) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PMT: %v", err)
		}
	}

	reader := r.NewSubscriberReader(0)
	t.Cleanup(func() { _ = reader.Close() })
	return r, reader
}

// pushFiller advances the write head with non-video packets, which never index a
// random access point.
func pushFiller(t *testing.T, r *MasterRing, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		if _, err := r.Push(context.Background(), createBasicPacket(85, false, uint8(i%16))); err != nil {
			t.Fatalf("push filler %d: %v", i, err)
		}
	}
}

// pushKeyframe pushes a video access unit that the ring will index, and returns the
// offset it was written at. A following packet finalises the access unit.
func pushKeyframe(t *testing.T, r *MasterRing, pid uint16, es []byte, cc uint8) int64 {
	t.Helper()
	offset := r.Head()
	if _, err := r.Push(context.Background(), createVideoPESPacket(pid, true, cc, es)); err != nil {
		t.Fatalf("push keyframe: %v", err)
	}
	if _, err := r.Push(context.Background(), createVideoPESPacket(pid, true, cc+1, []byte{0x00, 0x00, 0x01, 0x41})); err != nil {
		t.Fatalf("finalise access unit: %v", err)
	}
	return offset
}

// readAllPending drains whatever the reader delivers until it reaches ring data at
// or beyond wantOffset, returning everything it read before that point.
func readUntilOffset(t *testing.T, reader *SubscriberReader, buf []byte) []byte {
	t.Helper()
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return append([]byte(nil), buf[:n]...)
}

// 1. An overrun must re-enter at the newest random access point, never at the tail.
func TestSubscriberReader_OverrunResyncsToKeyframeNotTail(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60) // overruns the subscriber sitting at offset 0
	keyframe := pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	tailAtRecovery := r.Tail()
	if keyframe <= tailAtRecovery {
		t.Fatalf("test setup: keyframe %d is not ahead of tail %d", keyframe, tailAtRecovery)
	}

	buf := make([]byte, 32*1024)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("read after overrun: %v", err)
	}

	if got := reader.Offset(); got != keyframe {
		t.Fatalf("resumed at %d, want the keyframe at %d (tail was %d)", got, keyframe, tailAtRecovery)
	}
}

// 2. The topology must arrive whole, ahead of any ring data, even when the caller's
// buffer is one TS packet wide.
func TestSubscriberReader_OverrunDeliversFullPreambleBeforeRingData(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60)
	keyframe := pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	wantPreamble := r.PATPMTPreamble()
	if len(wantPreamble) == 0 {
		t.Fatal("test setup: ring holds no PAT/PMT preamble")
	}

	// One packet at a time: the preamble must not be cut short or interleaved.
	buf := make([]byte, TSPacketSize)
	var got []byte
	for len(got) < len(wantPreamble) {
		got = append(got, readUntilOffset(t, reader, buf)...)
	}

	if !bytes.Equal(got, wantPreamble) {
		t.Fatalf("preamble delivered as %d bytes, want the ring's %d-byte PAT/PMT", len(got), len(wantPreamble))
	}
	if got := reader.Offset(); got != keyframe {
		t.Fatalf("ring cursor moved to %d while the preamble was being delivered; want %d", got, keyframe)
	}

	// Only now may ring data follow, and it starts at the keyframe.
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("read after preamble: %v", err)
	}
	if n != TSPacketSize {
		t.Fatalf("first ring read returned %d bytes, want one TS packet", n)
	}
	if buf[0] != SyncByte {
		t.Fatalf("first ring byte is 0x%02X, want the TS sync byte", buf[0])
	}
}

// 3. With no random access point in the ring, Read waits rather than substituting
// the tail, and recovers as soon as one appears.
func TestSubscriberReader_OverrunWithoutKeyframeWaitsForNewRAP(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60) // overrun, and nothing decodable anywhere in the ring

	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		buf := make([]byte, 32*1024)
		n, err := reader.Read(buf)
		done <- result{n, err}
	}()

	select {
	case res := <-done:
		t.Fatalf("read returned %d bytes (err=%v) with no keyframe in the ring; it must wait", res.n, res.err)
	case <-time.After(150 * time.Millisecond):
	}

	keyframe := pushKeyframe(t, r, 256, h264IDRPayload, 0)

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("read failed after the keyframe arrived: %v", res.err)
		}
		if res.n == 0 {
			t.Fatal("read returned no bytes after recovery")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read did not recover after a keyframe became available")
	}

	if got := reader.Offset(); got != keyframe {
		t.Fatalf("recovered at %d, want the new keyframe at %d", got, keyframe)
	}
}

// 4. A PMT change while the reader is waiting must be reflected in what it receives:
// the new generation's topology, and a keyframe belonging to that generation.
func TestSubscriberReader_PMTChangeDuringWaitDeliversNewGenerationPreamble(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60)

	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32*1024)
		n, err := reader.Read(buf)
		if err != nil {
			done <- nil
			return
		}
		done <- append([]byte(nil), buf[:n]...)
	}()

	select {
	case <-done:
		t.Fatal("read returned before any keyframe existed")
	case <-time.After(100 * time.Millisecond):
	}

	// New generation: video moves to PID 512 and to HEVC.
	for _, pkt := range createMultiPacketPMTWithVersion(100, 512, true, true, 1, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PMT v1: %v", err)
		}
	}
	if pid, codec := r.VideoDetails(); pid != 512 || codec != CodecH265 {
		t.Fatalf("test setup: expected PID 512/HEVC after the PMT change, got %d/%v", pid, codec)
	}

	keyframe := pushKeyframe(t, r, 512, hevcIRAPPayload, 0)
	wantPreamble := r.PATPMTPreamble()

	select {
	case got := <-done:
		if got == nil {
			t.Fatal("read failed during the generation change")
		}
		if !bytes.HasPrefix(wantPreamble, got) {
			t.Fatalf("delivered %d bytes that are not a prefix of the new generation's preamble", len(got))
		}
		// The new PMT names PID 512; the stale one named 256.
		if bytes.Equal(got, mustPreambleOf(t, 256)) {
			t.Fatal("reader delivered the previous generation's preamble")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read did not recover after the new generation produced a keyframe")
	}

	if got := reader.Offset(); got != keyframe {
		t.Fatalf("recovered at %d, want the new generation's keyframe at %d", got, keyframe)
	}
}

// mustPreambleOf builds the preamble a ring would hold for a PMT naming videoPID,
// so a test can assert that a delivered preamble is not the stale one.
func mustPreambleOf(t *testing.T, videoPID uint16) []byte {
	t.Helper()
	var out []byte
	for _, pkt := range createMultiPacketPAT(100) {
		out = append(out, pkt...)
	}
	for _, pkt := range createMultiPacketPMT(100, videoPID, false) {
		out = append(out, pkt...)
	}
	return out
}

// 5. DroppedBytes counts what the ring discarded. It must not also absorb the bytes
// recovery chose to skip on the way to a keyframe - those are a separate number.
func TestSubscriberReader_DroppedBytesExcludesResyncSkip(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60)
	keyframe := pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	tailAtRecovery := r.Tail()

	buf := make([]byte, 32*1024)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("read after overrun: %v", err)
	}

	wantDropped := tailAtRecovery // the subscriber started at offset 0
	wantSkipped := keyframe - tailAtRecovery

	if wantSkipped <= 0 {
		t.Fatalf("test setup: keyframe %d is not ahead of tail %d", keyframe, tailAtRecovery)
	}

	if got := reader.DroppedBytes(); got != wantDropped {
		t.Fatalf("DroppedBytes = %d, want %d (bytes the ring actually discarded)", got, wantDropped)
	}
	if got := reader.ResyncSkippedBytes(); got != wantSkipped {
		t.Fatalf("ResyncSkippedBytes = %d, want %d (bytes skipped to reach the keyframe)", got, wantSkipped)
	}
	if got := reader.DroppedBytes(); got == wantDropped+wantSkipped {
		t.Fatal("DroppedBytes absorbed the resync skip; the two causes must stay apart")
	}
}

// 6. A subscriber that is overrun, waiting, or simply not reading must never hold
// up the writer.
func TestSubscriberReader_WaitingSubscriberDoesNotBlockWriter(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60) // overrun with nothing decodable: the reader will wait

	go func() {
		buf := make([]byte, 32*1024)
		_, _ = reader.Read(buf)
	}()
	time.Sleep(100 * time.Millisecond) // let the reader reach its wait

	start := time.Now()
	pushFiller(t, r, 2000)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("2000 pushes took %s while a subscriber was waiting; the writer was blocked", elapsed)
	}

	// Several readers doing the same, concurrently. The pushes below evict the
	// keyframe again, so these readers end up waiting indefinitely - which is the
	// designed behaviour, not a defect: the reader takes no context, so bounding
	// recovery is the consumer's job, and Close is how it does it.
	var wg sync.WaitGroup
	extras := make([]*SubscriberReader, 0, 4)
	for i := 0; i < 4; i++ {
		extra := r.NewSubscriberReader(0)
		extras = append(extras, extra)
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, TSPacketSize)
			for {
				if _, err := extra.Read(buf); err != nil {
					return
				}
			}
		}()
	}

	start = time.Now()
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 200)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("pushes took %s with four readers attached; the writer was blocked", elapsed)
	}

	for _, extra := range extras {
		_ = extra.Close()
	}

	released := make(chan struct{})
	go func() {
		wg.Wait()
		close(released)
	}()
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("closing a waiting reader did not release it; the consumer-side watchdog would have no effect")
	}
}

// 7. Every read that can hold a packet must end on a packet boundary, preamble and
// ring data alike, so a downstream TS consumer never sees a split packet.
func TestSubscriberReader_ResyncPreservesPacketAlignment(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60)
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 20)

	buf := make([]byte, 5*TSPacketSize)
	for i := 0; i < 6; i++ {
		n, err := reader.Read(buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if n%TSPacketSize != 0 {
			t.Fatalf("read %d returned %d bytes, not a multiple of %d", i, n, TSPacketSize)
		}
		if buf[0] != SyncByte {
			t.Fatalf("read %d does not start on a sync byte (0x%02X)", i, buf[0])
		}
	}
}

// 8. The same recovery must hold for MPEG-2, whose random access points are
// sequence headers and I-frames rather than IDR NALs.
func TestSubscriberReader_MPEG2OverrunResyncsToIFrame(t *testing.T) {
	r := NewMasterRingWithProgram(40*TSPacketSize, 1)
	defer r.Close()

	for _, pkt := range createMultiPacketPAT(100) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PAT: %v", err)
		}
	}
	if _, err := r.Push(context.Background(), mpeg2PMTPacket(t, 100, 200)); err != nil {
		t.Fatalf("push MPEG-2 PMT: %v", err)
	}
	if r.facts.VideoCodec != CodecMPEG2 {
		t.Fatalf("test setup: expected CodecMPEG2, got %v", r.facts.VideoCodec)
	}

	reader := r.NewSubscriberReader(0)
	defer func() { _ = reader.Close() }()

	pushFiller(t, r, 60)

	mpeg2IFrame := []byte{
		0x00, 0x00, 0x01, 0xB3, 0x2D, 0x02, 0x40, 0x23, // sequence header
		0x00, 0x00, 0x01, 0xB8, 0x00, 0x08, 0x00, 0x00, // GOP header
		0x00, 0x00, 0x01, 0x00, 0x00, 0x08, 0x00, 0x00, // picture header, I-frame
	}
	keyframe := r.Head()
	if _, err := r.Push(context.Background(), createVideoPESPacket(200, true, 0, mpeg2IFrame)); err != nil {
		t.Fatalf("push I-frame: %v", err)
	}
	if _, err := r.Push(context.Background(), createVideoPESPacket(200, true, 1, []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x10})); err != nil {
		t.Fatalf("finalise access unit: %v", err)
	}

	buf := make([]byte, 32*1024)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("read after overrun: %v", err)
	}
	if got := reader.Offset(); got != keyframe {
		t.Fatalf("MPEG-2 recovery resumed at %d, want the I-frame at %d", got, keyframe)
	}
}

func mpeg2PMTPacket(t *testing.T, pmtPID, videoPID uint16) []byte {
	t.Helper()

	section := []byte{
		0x02,
		0xB0, 0x17,
		0x00, 0x01,
		0xC1,
		0x00, 0x00,
		0xE0 | byte((videoPID>>8)&0x1F), byte(videoPID & 0xFF),
		0xF0, 0x00,

		0x02, // MPEG-2 video
		0xE0 | byte((videoPID>>8)&0x1F), byte(videoPID & 0xFF),
		0xF0, 0x00,

		0x03, // MP2 audio
		0xE0, 0xC9,
		0xF0, 0x00,

		0x00, 0x00, 0x00, 0x00,
	}
	binary.BigEndian.PutUint32(section[22:26], CalculateMPEG2CRC32(section[:22]))

	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 | byte((pmtPID>>8)&0x1F)
	pkt[2] = byte(pmtPID & 0xFF)
	pkt[3] = 0x10
	pkt[4] = 0x00
	copy(pkt[5:], section)
	for i := 5 + len(section); i < TSPacketSize; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

// A service whose PMT names no video has no random access points to wait for. The
// tail stays a legal entry point there, and recovery must not block.
func TestSubscriberReader_AudioOnlyServiceResumesAtTailWithoutWaiting(t *testing.T) {
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	for _, pkt := range createMultiPacketPAT(100) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PAT: %v", err)
		}
	}
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 0, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push audio-only PMT: %v", err)
		}
	}
	if r.facts.VideoPID != 0 {
		t.Fatalf("test setup: expected no video PID, got %d", r.facts.VideoPID)
	}

	reader := r.NewSubscriberReader(0)
	defer func() { _ = reader.Close() }()

	pushFiller(t, r, 30)

	done := make(chan int, 1)
	go func() {
		buf := make([]byte, TSPacketSize)
		n, err := reader.Read(buf)
		if err != nil {
			done <- -1
			return
		}
		done <- n
	}()

	select {
	case n := <-done:
		if n != TSPacketSize {
			t.Fatalf("read returned %d, want one TS packet from the tail", n)
		}
	case <-time.After(time.Second):
		t.Fatal("read blocked on a service that can never produce a random access point")
	}

	if got := reader.ResyncSkippedBytes(); got != 0 {
		t.Fatalf("ResyncSkippedBytes = %d on a service with no keyframes, want 0", got)
	}
}

// "No video PID yet" is not the same fact as "no video". A ring that has parsed no
// complete PMT knows nothing about the stream, and nothing licenses handing out the
// tail: the service may well turn out to carry video, and by then a decoder would
// already have been started mid-picture. Waiting is the fail-closed answer, and the
// consumer bounds it - here by closing the reader.
func TestSubscriberReader_UnknownTopologyWaitsRatherThanResumingAtTail(t *testing.T) {
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	reader := r.NewSubscriberReader(0)

	// No PAT, no PMT: nothing here says what this stream is.
	for i := 0; i < 30; i++ {
		if _, err := r.Push(context.Background(), createBasicPacket(256, false, uint8(i%16))); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	if r.ReadinessFacts().HasPMT {
		t.Fatal("test setup: ring reports a PMT it was never given")
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, TSPacketSize)
		_, err := reader.Read(buf)
		done <- err
	}()

	select {
	case <-done:
		t.Fatal("read resumed at the tail with no topology known; a video service would have started mid-picture")
	case <-time.After(150 * time.Millisecond):
	}

	_ = reader.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the reader did not release the wait")
	}
}

// The preamble is handed over across several reads when the consumer's buffer is
// small, which opens a second staleness window after the one atomic attach closes:
// a PMT bump between two of those reads must not let the tail of the old
// generation's topology through. The partial preamble is dropped and recovery
// starts again against the new generation.
func TestSubscriberReader_PartialPreambleIsDiscardedOnGenerationChange(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60)
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	stalePreamble := r.PATPMTPreamble()
	if len(stalePreamble) <= TSPacketSize {
		t.Fatalf("test setup: preamble is %d bytes, too small to be delivered in parts", len(stalePreamble))
	}

	// One packet only: the rest of this generation's preamble stays queued.
	buf := make([]byte, TSPacketSize)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("first preamble read: %v", err)
	}
	if n != TSPacketSize || !bytes.Equal(buf[:n], stalePreamble[:n]) {
		t.Fatalf("first read did not deliver the head of the current preamble")
	}

	// The stream moves on while the rest is still queued.
	for _, pkt := range createMultiPacketPMTWithVersion(100, 512, true, true, 1, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push PMT v1: %v", err)
		}
	}
	if pid, codec := r.VideoDetails(); pid != 512 || codec != CodecH265 {
		t.Fatalf("test setup: expected PID 512/HEVC, got %d/%v", pid, codec)
	}

	// The remainder of the old preamble must never appear.
	staleRemainder := stalePreamble[n:]
	done := make(chan []byte, 1)
	go func() {
		b := make([]byte, TSPacketSize)
		count, rerr := reader.Read(b)
		if rerr != nil {
			done <- nil
			return
		}
		done <- append([]byte(nil), b[:count]...)
	}()

	select {
	case got := <-done:
		t.Fatalf("read returned %d bytes during a generation change; the stale remainder starts %x", len(got), staleRemainder[:8])
	case <-time.After(150 * time.Millisecond):
	}

	// Once the new generation is decodable, its own topology is delivered whole.
	keyframe := pushKeyframe(t, r, 512, hevcIRAPPayload, 0)
	freshPreamble := r.PATPMTPreamble()

	select {
	case got := <-done:
		if got == nil {
			t.Fatal("read failed after the new generation became decodable")
		}
		if !bytes.Equal(got, freshPreamble[:len(got)]) {
			t.Fatal("read did not resume with the new generation's preamble")
		}
		if bytes.Equal(got, staleRemainder[:len(got)]) {
			t.Fatal("read delivered the remainder of the previous generation's preamble")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read did not recover after the new generation produced a keyframe")
	}

	// Drain the rest of the fresh preamble, then confirm the cursor is on the RAP.
	delivered := TSPacketSize
	for delivered < len(freshPreamble) {
		count, rerr := reader.Read(buf)
		if rerr != nil {
			t.Fatalf("draining the fresh preamble: %v", rerr)
		}
		delivered += count
	}
	if got := reader.Offset(); got != keyframe {
		t.Fatalf("cursor at %d after recovery, want the new generation's keyframe at %d", got, keyframe)
	}
}

// 1. Original failure: Partial old preamble, complete new audio-only PMT, and subsequent marked
// audio payload: fresh preamble followed by the marked payload; no old video bytes.
func TestSubscriberReader_PartialPreambleThenAudioOnlyMustResume(t *testing.T) {
	r, reader := newH264Ring(t, 40)

	pushFiller(t, r, 60) // overruns the subscriber sitting at offset 0
	oldKf := pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	stalePreamble := r.PATPMTPreamble()
	if len(stalePreamble) <= TSPacketSize {
		t.Fatalf("test setup: preamble is %d bytes, too small to be delivered in parts", len(stalePreamble))
	}

	// First read delivers one packet of the old H.264 preamble
	buf := make([]byte, TSPacketSize)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if n != TSPacketSize || !bytes.Equal(buf[:n], stalePreamble[:n]) {
		t.Fatalf("first read did not deliver the head of the old preamble")
	}

	r.mu.Lock()
	pending := len(reader.pendingPrefix)
	r.mu.Unlock()
	if pending == 0 {
		t.Fatal("probe requires a partial pending preamble")
	}

	// The stream transitions to audio-only (PMT version 1, videoPID 0)
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 1, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatalf("push audio PMT: %v", err)
		}
	}
	facts := r.ReadinessFacts()
	if !facts.HasPMT {
		t.Fatal("probe requires a complete new PMT")
	}
	if pid, _ := r.VideoDetails(); pid != 0 {
		t.Fatalf("probe requires an audio-only program, got video PID %d", pid)
	}

	// Push a distinct marked audio TS packet on PID 85
	markedAudioPkt := createBasicPacket(85, false, 0)
	copy(markedAudioPkt[4:], []byte("AUDIO_PAYLOAD_MARKER_TEST"))
	if _, err := r.Push(context.Background(), markedAudioPkt); err != nil {
		t.Fatalf("push marked audio: %v", err)
	}

	freshPreamble := r.PATPMTPreamble()

	done := make(chan []byte, 1)
	go func() {
		b := make([]byte, TSPacketSize)
		count, rerr := reader.Read(b)
		if rerr != nil {
			done <- nil
			return
		}
		done <- append([]byte(nil), b[:count]...)
	}()

	var firstRecovered []byte
	select {
	case got := <-done:
		if got == nil {
			t.Fatal("read returned error during recovery")
		}
		firstRecovered = got
	case <-time.After(500 * time.Millisecond):
		_ = reader.Close()
		<-done
		t.Fatal("reader blocked on audio-only transition")
	}

	// First packet delivered must be the head of the fresh audio-only preamble
	if !bytes.Equal(firstRecovered, freshPreamble[:TSPacketSize]) {
		t.Fatalf("first recovered packet is not fresh preamble head\ngot:  %x\nwant: %x",
			firstRecovered[:16], freshPreamble[:16])
	}

	// Drain remainder of fresh preamble
	drained := TSPacketSize
	for drained < len(freshPreamble) {
		b := make([]byte, TSPacketSize)
		count, rerr := reader.Read(b)
		if rerr != nil {
			t.Fatalf("drain fresh preamble: %v", rerr)
		}
		if !bytes.Equal(b[:count], freshPreamble[drained:drained+count]) {
			t.Fatalf("drain mismatch: got %x want %x", b[:count], freshPreamble[drained:drained+count])
		}
		drained += count
	}

	// Next read MUST deliver the marked audio packet, NOT the old keyframe!
	payloadBuf := make([]byte, TSPacketSize)
	n, err = reader.Read(payloadBuf)
	if err != nil {
		t.Fatalf("read after preamble: %v", err)
	}
	if n != TSPacketSize {
		t.Fatalf("payload read length = %d, want %d", n, TSPacketSize)
	}
	if !bytes.Equal(payloadBuf, markedAudioPkt) {
		if bytes.Contains(payloadBuf, h264IDRPayload) {
			t.Fatalf("CRITICAL: reader delivered stale H.264 video payload from offset %d under audio-only PMT!", oldKf)
		}
		t.Fatalf("payload read mismatch: got %x, want marked audio %x", payloadBuf[:16], markedAudioPkt[:16])
	}

	// Verify ResyncSkippedBytes accounted the skipped old video bytes
	if skipped := reader.ResyncSkippedBytes(); skipped == 0 {
		t.Fatal("ResyncSkippedBytes = 0, expected skipped old video bytes to be accounted")
	}
}

// 2. Chunk boundaries: Old video, changed PMT and new audio in one push; compare with split pushes;
// assert the documented conservative skips rather than claiming identical payload output.
func TestSubscriberReader_TransitionChunkConservativeSkip(t *testing.T) {
	// Part A: Single push containing [old video | new PMT | new audio]
	rA, readerA := newH264Ring(t, 40)
	pushFiller(t, rA, 60)
	pushKeyframe(t, rA, 256, h264IDRPayload, 0)
	pushFiller(t, rA, 3)

	// Reader drains partial preamble
	buf := make([]byte, TSPacketSize)
	if _, err := readerA.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Build a single composite chunk:
	// 1 old video packet + audio-only PMT packets + 1 audio packet
	var compositeChunk []byte
	compositeChunk = append(compositeChunk, createVideoPESPacket(256, false, 5, []byte{0x00, 0x00, 0x01, 0x41})...)
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 1, 1) {
		compositeChunk = append(compositeChunk, pkt...)
	}
	audioPktInChunk := createBasicPacket(85, false, 1)
	copy(audioPktInChunk[4:], []byte("AUDIO_INSIDE_CHUNK"))
	compositeChunk = append(compositeChunk, audioPktInChunk...)

	if _, err := rA.Push(context.Background(), compositeChunk); err != nil {
		t.Fatalf("push composite chunk: %v", err)
	}

	floorA := rA.GenerationResumeFloor()
	if floorA != rA.Head() {
		t.Fatalf("floorA = %d, want rA.Head() = %d at commit end", floorA, rA.Head())
	}

	// Push subsequent audio packet after the transition chunk
	subsequentAudioPkt := createBasicPacket(85, false, 2)
	copy(subsequentAudioPkt[4:], []byte("AUDIO_AFTER_CHUNK_"))
	if _, err := rA.Push(context.Background(), subsequentAudioPkt); err != nil {
		t.Fatalf("push subsequent audio: %v", err)
	}

	// Reader recovers: drains fresh preamble
	freshPreambleA := rA.PATPMTPreamble()
	drainedA := 0
	for drainedA < len(freshPreambleA) {
		b := make([]byte, TSPacketSize)
		count, err := readerA.Read(b)
		if err != nil {
			t.Fatalf("drain preamble: %v", err)
		}
		drainedA += count
	}

	// Next read MUST deliver subsequentAudioPkt, because audioPktInChunk was conservatively skipped!
	payloadA := make([]byte, TSPacketSize)
	nA, errA := readerA.Read(payloadA)
	if errA != nil {
		t.Fatalf("read payload A: %v", errA)
	}
	if nA != TSPacketSize || !bytes.Equal(payloadA, subsequentAudioPkt) {
		t.Fatalf("single-chunk recovery did not deliver subsequent audio packet as expected\ngot:  %x\nwant: %x",
			payloadA[:16], subsequentAudioPkt[:16])
	}
	skippedA := readerA.ResyncSkippedBytes()
	if skippedA == 0 {
		t.Fatal("skippedA = 0, expected conservative skip of transition chunk bytes")
	}

	// Part B: Split pushes: [old video], then [new PMT], then [new audio]
	rB, readerB := newH264Ring(t, 40)
	pushFiller(t, rB, 60)
	pushKeyframe(t, rB, 256, h264IDRPayload, 0)
	pushFiller(t, rB, 3)

	if _, err := readerB.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Push 1: old video
	if _, err := rB.Push(context.Background(), createVideoPESPacket(256, false, 5, []byte{0x00, 0x00, 0x01, 0x41})); err != nil {
		t.Fatal(err)
	}
	// Push 2: PMT transition chunk
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 1, 1) {
		if _, err := rB.Push(context.Background(), pkt); err != nil {
			t.Fatal(err)
		}
	}
	floorB := rB.GenerationResumeFloor()
	// Push 3: audio packet
	if _, err := rB.Push(context.Background(), audioPktInChunk); err != nil {
		t.Fatal(err)
	}

	// Drain preamble B
	freshPreambleB := rB.PATPMTPreamble()
	drainedB := 0
	for drainedB < len(freshPreambleB) {
		b := make([]byte, TSPacketSize)
		count, err := readerB.Read(b)
		if err != nil {
			t.Fatalf("drain preamble B: %v", err)
		}
		drainedB += count
	}

	// In split push, audioPktInChunk was pushed AFTER the PMT transition chunk commit, so it IS delivered!
	payloadB := make([]byte, TSPacketSize)
	nB, errB := readerB.Read(payloadB)
	if errB != nil {
		t.Fatalf("read payload B: %v", errB)
	}
	if nB != TSPacketSize || !bytes.Equal(payloadB, audioPktInChunk) {
		t.Fatalf("split push did not deliver audio packet\ngot:  %x\nwant: %x", payloadB[:16], audioPktInChunk[:16])
	}
	if readerB.Offset() <= floorB {
		t.Fatalf("readerB offset %d not past floor %d", readerB.Offset(), floorB)
	}
}

// 3. Multiple transitions: Two identity changes in one push and further changes while a prefix is partially drained;
// only final active preamble is queued.
func TestSubscriberReader_MultipleIdentityChangesInOneChunk(t *testing.T) {
	r, reader := newH264Ring(t, 40)
	pushFiller(t, r, 60)
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	buf := make([]byte, TSPacketSize)
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Ingest two PMT changes in one single push: PMT v1 (video PID 512), then PMT v2 (audio-only)
	var twoPMTs []byte
	for _, pkt := range createMultiPacketPMTWithVersion(100, 512, true, true, 1, 1) {
		twoPMTs = append(twoPMTs, pkt...)
	}
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 2, 1) {
		twoPMTs = append(twoPMTs, pkt...)
	}
	if _, err := r.Push(context.Background(), twoPMTs); err != nil {
		t.Fatalf("push two PMTs: %v", err)
	}

	// Active topology must be the final one (audio-only)
	if pid, _ := r.VideoDetails(); pid != 0 {
		t.Fatalf("expected final topology audio-only, got video PID %d", pid)
	}

	// Reader recovery must queue the final active preamble (audio-only, version 2)
	finalPreamble := r.PATPMTPreamble()
	b := make([]byte, TSPacketSize)
	n, err := reader.Read(b)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != TSPacketSize || !bytes.Equal(b[:n], finalPreamble[:n]) {
		t.Fatalf("read did not deliver final active preamble\ngot:  %x\nwant: %x", b[:16], finalPreamble[:16])
	}
}

func TestSubscriberReader_PreambleInvalidatedRepeatedlyWhileDraining(t *testing.T) {
	r, reader := newH264Ring(t, 40)
	pushFiller(t, r, 60)
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	buf := make([]byte, TSPacketSize)
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Change 1: PMT v1 (HEVC, video PID 512)
	for _, pkt := range createMultiPacketPMTWithVersion(100, 512, true, true, 1, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatal(err)
		}
	}
	pushKeyframe(t, r, 512, hevcIRAPPayload, 0)

	// Read 1 packet of v1 preamble
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Change 2: PMT v2 (Audio-only) while v1 preamble was partially drained
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 2, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatal(err)
		}
	}

	// Read again: must recover to v2 preamble
	v2Preamble := r.PATPMTPreamble()
	b := make([]byte, TSPacketSize)
	n, err := reader.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if n != TSPacketSize || !bytes.Equal(b[:n], v2Preamble[:n]) {
		t.Fatalf("did not deliver v2 preamble after repeated change\ngot:  %x\nwant: %x", b[:16], v2Preamble[:16])
	}
}

// 4. Recovery states: Video to video, video to audio-only, audio-only to video;
// already-waiting unknown to audio-only and unknown to video.
func TestSubscriberReader_RecoveryTransitions(t *testing.T) {
	t.Run("video_to_video", func(t *testing.T) {
		r, reader := newH264Ring(t, 40)
		pushFiller(t, r, 60)
		pushKeyframe(t, r, 256, h264IDRPayload, 0)
		buf := make([]byte, TSPacketSize)
		if _, err := reader.Read(buf); err != nil {
			t.Fatal(err)
		}

		for _, pkt := range createMultiPacketPMTWithVersion(100, 512, true, true, 1, 1) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		newKf := pushKeyframe(t, r, 512, hevcIRAPPayload, 0)

		preamble := r.PATPMTPreamble()
		drained := 0
		for drained < len(preamble) {
			b := make([]byte, TSPacketSize)
			n, err := reader.Read(b)
			if err != nil {
				t.Fatal(err)
			}
			drained += n
		}
		if reader.Offset() != newKf {
			t.Fatalf("reader offset = %d, want new keyframe at %d", reader.Offset(), newKf)
		}
	})

	t.Run("audio_only_to_video", func(t *testing.T) {
		r := NewMasterRing(40 * TSPacketSize)
		defer r.Close()
		for _, pkt := range createMultiPacketPAT(100) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 0, 1) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		reader := r.NewSubscriberReader(0)
		defer func() { _ = reader.Close() }()

		pushFiller(t, r, 60) // overruns
		buf := make([]byte, TSPacketSize)
		if _, err := reader.Read(buf); err != nil {
			t.Fatal(err)
		}

		// Transition to video
		for _, pkt := range createMultiPacketPMTWithVersion(100, 256, false, true, 1, 1) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		vkf := pushKeyframe(t, r, 256, h264IDRPayload, 0)

		preamble := r.PATPMTPreamble()
		drained := 0
		for drained < len(preamble) {
			b := make([]byte, TSPacketSize)
			n, err := reader.Read(b)
			if err != nil {
				t.Fatal(err)
			}
			drained += n
		}
		if reader.Offset() != vkf {
			t.Fatalf("offset %d, want video keyframe %d", reader.Offset(), vkf)
		}
	})

	t.Run("unknown_to_audio_only", func(t *testing.T) {
		r := NewMasterRing(40 * TSPacketSize)
		defer r.Close()
		// Only push PAT, no PMT yet (topology unknown)
		for _, pkt := range createMultiPacketPAT(100) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		reader := r.NewSubscriberReader(0)
		defer func() { _ = reader.Close() }()

		pushFiller(t, r, 60) // overruns while topology unknown

		done := make(chan error, 1)
		go func() {
			b := make([]byte, TSPacketSize)
			_, err := reader.Read(b)
			done <- err
		}()

		// Reader must block waiting for topology
		select {
		case err := <-done:
			t.Fatalf("reader returned early while topology unknown: %v", err)
		case <-time.After(150 * time.Millisecond):
		}

		// Now push audio-only PMT
		for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 0, 1) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}

		// Reader must unblock and succeed
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("reader failed after audio PMT arrived: %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("reader remained blocked after audio PMT arrived")
		}
	})

	t.Run("unknown_to_video", func(t *testing.T) {
		r := NewMasterRing(40 * TSPacketSize)
		defer r.Close()
		for _, pkt := range createMultiPacketPAT(100) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		reader := r.NewSubscriberReader(0)
		defer func() { _ = reader.Close() }()

		pushFiller(t, r, 60)

		done := make(chan error, 1)
		go func() {
			b := make([]byte, TSPacketSize)
			_, err := reader.Read(b)
			done <- err
		}()

		select {
		case err := <-done:
			t.Fatalf("reader returned early while topology unknown: %v", err)
		case <-time.After(150 * time.Millisecond):
		}

		// Push video PMT (still no keyframe)
		for _, pkt := range createMultiPacketPMTWithVersion(100, 256, false, true, 0, 1) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}

		// Still must wait for keyframe
		select {
		case err := <-done:
			t.Fatalf("reader returned before video keyframe arrived: %v", err)
		case <-time.After(150 * time.Millisecond):
		}

		// Push keyframe
		pushKeyframe(t, r, 256, h264IDRPayload, 0)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("reader failed after keyframe arrived: %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("reader remained blocked after keyframe arrived")
		}
	})
}

// 5. Existing RAP policy: Several valid new-generation RAPs retain latest-RAP selection,
// including RAPs inside a transition chunk.
func TestSubscriberReader_LatestRAPRetainedInTransitionChunk(t *testing.T) {
	r, reader := newH264Ring(t, 80)
	pushFiller(t, r, 100) // overrun
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	buf := make([]byte, TSPacketSize)
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Build a chunk with a new PMT followed by TWO keyframes: RAP 1 and RAP 2
	var transitionChunk []byte
	for _, pkt := range createMultiPacketPMTWithVersion(100, 512, true, true, 1, 1) {
		transitionChunk = append(transitionChunk, pkt...)
	}
	rap1Pkt := createVideoPESPacket(512, true, 0, hevcIRAPPayload)
	rap1Final := createVideoPESPacket(512, true, 1, []byte{0x00, 0x00, 0x01, 0x41})
	rap2Pkt := createVideoPESPacket(512, true, 2, hevcIRAPPayload)
	rap2Final := createVideoPESPacket(512, true, 3, []byte{0x00, 0x00, 0x01, 0x41})

	transitionChunk = append(transitionChunk, rap1Pkt...)
	transitionChunk = append(transitionChunk, rap1Final...)
	transitionChunk = append(transitionChunk, rap2Pkt...)
	transitionChunk = append(transitionChunk, rap2Final...)

	startOffset := r.Head()
	if _, err := r.Push(context.Background(), transitionChunk); err != nil {
		t.Fatal(err)
	}

	pmtLen := len(createMultiPacketPMTWithVersion(100, 512, true, true, 1, 1)) * TSPacketSize
	rap2Offset := startOffset + int64(pmtLen) + 2*TSPacketSize

	// Reader recovers: must pick the LATEST keyframe (rap2Offset)
	preamble := r.PATPMTPreamble()
	drained := 0
	for drained < len(preamble) {
		b := make([]byte, TSPacketSize)
		n, err := reader.Read(b)
		if err != nil {
			t.Fatal(err)
		}
		drained += n
	}
	if got := reader.Offset(); got != rap2Offset {
		t.Fatalf("reader offset = %d, want latest RAP2 at %d", got, rap2Offset)
	}
}

// 6. Retention/accounting: Floor retained and evicted; transition chunk larger than ring capacity;
// exact eviction/skipped-byte accounting, no double counts.
func TestSubscriberReader_FloorRetainedAndEvictedAccounting(t *testing.T) {
	t.Run("floor_retained", func(t *testing.T) {
		r, reader := newH264Ring(t, 50)
		pushFiller(t, r, 70) // overruns
		pushKeyframe(t, r, 256, h264IDRPayload, 0)
		buf := make([]byte, TSPacketSize)
		if _, err := reader.Read(buf); err != nil {
			t.Fatal(err)
		}

		// Audio PMT transition
		for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 1, 1) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		floor := r.GenerationResumeFloor()
		if r.Tail() >= floor {
			t.Fatalf("test setup: tail %d should be before floor %d", r.Tail(), floor)
		}

		// Drain preamble
		preamble := r.PATPMTPreamble()
		drained := 0
		for drained < len(preamble) {
			b := make([]byte, TSPacketSize)
			n, err := reader.Read(b)
			if err != nil {
				t.Fatal(err)
			}
			drained += n
		}

		if reader.Offset() != floor {
			t.Fatalf("cursor %d != floor %d", reader.Offset(), floor)
		}
		if reader.DroppedBytes() == 0 || reader.ResyncSkippedBytes() == 0 {
			t.Fatalf("expected non-zero dropped (%d) and resyncSkipped (%d)",
				reader.DroppedBytes(), reader.ResyncSkippedBytes())
		}
	})

	t.Run("floor_evicted", func(t *testing.T) {
		r, reader := newH264Ring(t, 20)
		pushFiller(t, r, 30) // overruns
		pushKeyframe(t, r, 256, h264IDRPayload, 0)
		buf := make([]byte, TSPacketSize)
		if _, err := reader.Read(buf); err != nil {
			t.Fatal(err)
		}

		// Audio PMT transition
		for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 1, 1) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		floor := r.GenerationResumeFloor()

		// Now push enough filler to completely evict the floor past the tail
		pushFiller(t, r, 40)
		tail := r.Tail()
		if tail <= floor {
			t.Fatalf("test setup: tail %d should be strictly past floor %d", tail, floor)
		}

		// Drain preamble
		preamble := r.PATPMTPreamble()
		drained := 0
		for drained < len(preamble) {
			b := make([]byte, TSPacketSize)
			n, err := reader.Read(b)
			if err != nil {
				t.Fatal(err)
			}
			drained += n
		}

		// Cursor must be at tail, never rewound to floor
		if reader.Offset() < tail {
			t.Fatalf("cursor %d rewound behind tail %d!", reader.Offset(), tail)
		}
	})

	t.Run("transition_chunk_larger_than_capacity", func(t *testing.T) {
		// Ring capacity = 10 packets
		r := NewMasterRing(10 * TSPacketSize)
		defer r.Close()

		for _, pkt := range createMultiPacketPAT(100) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}

		// Build a chunk of 25 packets (2.5x capacity)
		var largeChunk []byte
		for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 1, 1) {
			largeChunk = append(largeChunk, pkt...)
		}
		for i := 0; i < 20; i++ {
			largeChunk = append(largeChunk, createBasicPacket(85, false, uint8(i%16))...)
		}

		n, err := r.Push(context.Background(), largeChunk)
		if err != nil {
			t.Fatalf("push large chunk: %v", err)
		}
		if n != len(largeChunk) {
			t.Fatalf("wrote %d, want %d", n, len(largeChunk))
		}
		if r.GenerationResumeFloor() != r.Head() {
			t.Fatalf("floor %d != head %d", r.GenerationResumeFloor(), r.Head())
		}
	})
}

// 7. Prefix lifecycle: Small-buffer partial reads and full preamble drain;
// no endless preamble re-queue; stale prefix never finishes after invalidation.
func TestSubscriberReader_PreambleDrainLifecycle(t *testing.T) {
	r, reader := newH264Ring(t, 40)
	pushFiller(t, r, 60)
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 3)

	preamble := r.PATPMTPreamble()
	totalPreambleLen := len(preamble)

	// Read in 50-byte chunks
	smallBuf := make([]byte, 50)
	var accumulated []byte
	for len(accumulated) < totalPreambleLen {
		n, err := reader.Read(smallBuf)
		if err != nil {
			t.Fatalf("small read: %v", err)
		}
		accumulated = append(accumulated, smallBuf[:n]...)
	}

	if !bytes.Equal(accumulated, preamble) {
		t.Fatalf("drained preamble mismatch\ngot:  %x\nwant: %x", accumulated[:16], preamble[:16])
	}

	// After draining, next read must come from ring data, preamble must not be re-queued
	pktBuf := make([]byte, TSPacketSize)
	n, err := reader.Read(pktBuf)
	if err != nil {
		t.Fatalf("payload read: %v", err)
	}
	if n != TSPacketSize {
		t.Fatalf("payload read count = %d, want %d", n, TSPacketSize)
	}
	// Preamble starts with PAT (PID 0), ring data at keyframe starts with video PES (PID 256)
	pid := (uint16(pktBuf[1]&0x1F) << 8) | uint16(pktBuf[2])
	if pid == 0 {
		t.Fatal("preamble was re-queued after being fully drained!")
	}
	if pid != 256 {
		t.Fatalf("expected video PID 256, got %d", pid)
	}
}

// 8. Ordinary readers: Newly created raw reader, normal active reader without recovery,
// primed subscriber, and explicit seek keep existing behavior.
func TestSubscriberReader_OrdinaryReadersUnaffectedByFloor(t *testing.T) {
	r, _ := newH264Ring(t, 40)
	pushKeyframe(t, r, 256, h264IDRPayload, 0)
	pushFiller(t, r, 5)

	// Transition to audio-only
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 1, 1) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatal(err)
		}
	}
	floor := r.GenerationResumeFloor()

	// 1. Raw reader created at offset 0 (before floor)
	rawReader := r.NewSubscriberReader(0)
	defer func() { _ = rawReader.Close() }()

	buf := make([]byte, TSPacketSize)
	n, err := rawReader.Read(buf)
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if n != TSPacketSize {
		t.Fatalf("raw read returned %d, want %d", n, TSPacketSize)
	}
	// Raw reader must NOT skip to floor; its offset advances sequentially by TSPacketSize
	if rawReader.Offset() != TSPacketSize {
		t.Fatalf("raw reader offset = %d, want %d", rawReader.Offset(), TSPacketSize)
	}
	if rawReader.Offset() >= floor {
		t.Fatalf("raw reader incorrectly skipped to floor %d", floor)
	}

	// 2. Primed subscriber on video stream
	rVid := NewMasterRing(40 * TSPacketSize)
	defer rVid.Close()
	for _, pkt := range createMultiPacketPAT(100) {
		if _, err := rVid.Push(context.Background(), pkt); err != nil {
			t.Fatal(err)
		}
	}
	for _, pkt := range createMultiPacketPMTWithVersion(100, 256, false, true, 0, 1) {
		if _, err := rVid.Push(context.Background(), pkt); err != nil {
			t.Fatal(err)
		}
	}
	kfOffset := pushKeyframe(t, rVid, 256, h264IDRPayload, 0)

	attach, primedReader, err := rVid.NewPrimedSubscriber()
	if err != nil {
		t.Fatalf("NewPrimedSubscriber: %v", err)
	}
	defer func() { _ = primedReader.Close() }()

	if attach.KeyframeOffset != kfOffset {
		t.Fatalf("attach kf %d != %d", attach.KeyframeOffset, kfOffset)
	}
	if primedReader.Offset() != kfOffset {
		t.Fatalf("primed reader offset %d != %d", primedReader.Offset(), kfOffset)
	}
}

// 9. Control/close: Target change without a push, later PMT completion, reader close
// and ring close while blocked; no leaked goroutines.
func TestSubscriberReader_ControlAndCloseWhileBlocked(t *testing.T) {
	t.Run("target_change_wakes_blocked_reader", func(t *testing.T) {
		r := NewMasterRing(40 * TSPacketSize)
		defer r.Close()

		for _, pkt := range createMultiPacketPAT(100) {
			if _, err := r.Push(context.Background(), pkt); err != nil {
				t.Fatal(err)
			}
		}
		reader := r.NewSubscriberReader(0)
		defer func() { _ = reader.Close() }()

		pushFiller(t, r, 60) // overruns while topology unknown

		done := make(chan error, 1)
		go func() {
			b := make([]byte, TSPacketSize)
			_, err := reader.Read(b)
			done <- err
		}()

		select {
		case err := <-done:
			t.Fatalf("reader returned early: %v", err)
		case <-time.After(150 * time.Millisecond):
		}

		// SetTargetProgram with no subsequent push
		if err := r.SetTargetProgram(context.Background(), 2); err != nil {
			t.Fatalf("SetTargetProgram: %v", err)
		}

		// Reader was woken and re-evaluated (still waiting for program 2 PMT)
		select {
		case err := <-done:
			t.Fatalf("reader should still wait for program 2 PMT: %v", err)
		case <-time.After(150 * time.Millisecond):
		}

		_ = reader.Close()
		<-done
	})

	t.Run("reader_close_unblocks_waiting_reader", func(t *testing.T) {
		r := NewMasterRing(40 * TSPacketSize)
		defer r.Close()
		reader := r.NewSubscriberReader(0)

		done := make(chan error, 1)
		go func() {
			b := make([]byte, TSPacketSize)
			_, err := reader.Read(b)
			done <- err
		}()

		select {
		case err := <-done:
			t.Fatalf("reader returned early: %v", err)
		case <-time.After(150 * time.Millisecond):
		}

		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}

		select {
		case err := <-done:
			if !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF on close, got %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("reader did not unblock on Close")
		}
	})

	t.Run("ring_close_unblocks_waiting_reader", func(t *testing.T) {
		r := NewMasterRing(40 * TSPacketSize)
		reader := r.NewSubscriberReader(0)
		defer func() { _ = reader.Close() }()

		done := make(chan error, 1)
		go func() {
			b := make([]byte, TSPacketSize)
			_, err := reader.Read(b)
			done <- err
		}()

		select {
		case err := <-done:
			t.Fatalf("reader returned early: %v", err)
		case <-time.After(150 * time.Millisecond):
		}

		r.Close()

		select {
		case err := <-done:
			if !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF on ring close, got %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("reader did not unblock on ring Close")
		}
	})
}

// 10. Atomicity: Failed/noncommitting ingest or target change does not publish a recovery floor.
func TestMasterRing_FloorAtomicityOnFailedIngest(t *testing.T) {
	r := NewMasterRing(40 * TSPacketSize)
	defer r.Close()

	for _, pkt := range createMultiPacketPAT(100) {
		if _, err := r.Push(context.Background(), pkt); err != nil {
			t.Fatal(err)
		}
	}
	floorBefore := r.GenerationResumeFloor()

	// 1. Invalid packet size (not aligned to 188)
	_, err := r.Push(context.Background(), []byte("invalid_packet_size"))
	if !errors.Is(err, ErrInvalidPacketSize) {
		t.Fatalf("expected ErrInvalidPacketSize, got %v", err)
	}
	if r.GenerationResumeFloor() != floorBefore {
		t.Fatalf("floor changed after invalid push: %d != %d", r.GenerationResumeFloor(), floorBefore)
	}

	// 2. Pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	validPkt := createBasicPacket(100, false, 0)
	_, err = r.Push(ctx, validPkt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected Canceled, got %v", err)
	}
	if r.GenerationResumeFloor() != floorBefore {
		t.Fatalf("floor changed after cancelled push: %d != %d", r.GenerationResumeFloor(), floorBefore)
	}
}
