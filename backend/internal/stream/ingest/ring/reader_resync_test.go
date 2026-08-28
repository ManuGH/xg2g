// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"context"
	"encoding/binary"
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
