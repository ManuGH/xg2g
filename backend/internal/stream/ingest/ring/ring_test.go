// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

func createBasicPacket(pid uint16, pusi bool, cc uint8) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F) // AFC=0x01
	return pkt
}

func createVideoPESPacket(pid uint16, pusi bool, cc uint8, esPayload []byte) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = byte((pid >> 8) & 0x1F)
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[2] = byte(pid & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F) // AFC=0x01

	payloadOffset := 4
	if pusi {
		// PES Header: 00 00 01 E0, len=0, flags=0x80, headerDataLen=5, PTS
		pkt[4] = 0x00
		pkt[5] = 0x00
		pkt[6] = 0x01
		pkt[7] = 0xE0
		pkt[8] = 0x00
		pkt[9] = 0x00
		pkt[10] = 0x80
		pkt[11] = 0x80
		pkt[12] = 0x05
		pkt[13] = 0x21
		pkt[14] = 0x00
		pkt[15] = 0x01
		pkt[16] = 0x00
		pkt[17] = 0x01
		payloadOffset = 18
	}

	copy(pkt[payloadOffset:], esPayload)
	return pkt
}

func createPATSectionBytes(progNum uint16, pmtPID uint16, version uint8, currentNext uint8, sectionNum uint8, lastSectionNum uint8) []byte {
	patSection := []byte{
		0x00,       // table_id = 0 (PAT) [0]
		0xB0, 0x0D, // section_syntax + length = 13 [1, 2]
		0x00, 0x01, // transport_stream_id = 1 [3, 4]
		0xC0 | ((version & 0x1F) << 1) | (currentNext & 0x01), // version + current_next [5]
		sectionNum,                               // section_number [6]
		lastSectionNum,                           // last_section_number [7]
		byte(progNum >> 8), byte(progNum & 0xFF), // program_number [8, 9]
		0xE0 | byte((pmtPID>>8)&0x1F), byte(pmtPID & 0xFF), // PMT PID [10, 11]
		0x00, 0x00, 0x00, 0x00, // CRC32 placeholder [12..15]
	}

	crc := CalculateMPEG2CRC32(patSection[:12])
	binary.BigEndian.PutUint32(patSection[12:16], crc)
	return patSection
}

func createMultiPacketPATWithVersion(pmtPID uint16, version uint8, currentNext uint8) [][]byte {
	patSection := createPATSectionBytes(1, pmtPID, version, currentNext, 0, 0)

	// Packet 1: PUSI=true, pointer_field=0, contains first 10 bytes of section + AF
	pkt1 := make([]byte, TSPacketSize)
	pkt1[0] = SyncByte
	pkt1[1] = 0x40 // PUSI=1, PID=0
	pkt1[2] = 0x00
	pkt1[3] = 0x30   // AFC=0x03, CC=0
	pkt1[4] = 172    // AF length
	pkt1[5] = 0x00   // AF flags
	pkt1[177] = 0x00 // pointer_field = 0
	copy(pkt1[178:], patSection[:10])

	// Packet 2: PUSI=false, contains remaining 6 bytes of section (CC=1)
	pkt2 := make([]byte, TSPacketSize)
	pkt2[0] = SyncByte
	pkt2[1] = 0x00 // PUSI=0, PID=0
	pkt2[2] = 0x00
	pkt2[3] = 0x11 // AFC=0x01, CC=1
	copy(pkt2[4:], patSection[10:])

	return [][]byte{pkt1, pkt2}
}

func createMultiPacketPAT(pmtPID uint16) [][]byte {
	return createMultiPacketPATWithVersion(pmtPID, 0, 1)
}

func createMultiProgramPAT(prog1, pmt1, prog2, pmt2 uint16) []byte {
	patSection := []byte{
		0x00,       // table_id = 0 (PAT)
		0xB0, 0x11, // section length = 17 (total 20 bytes)
		0x00, 0x01, // TS ID
		0xC1,       // version 0, current_next 1
		0x00, 0x00, // section 0 / last 0
		byte(prog1 >> 8), byte(prog1 & 0xFF),
		0xE0 | byte((pmt1>>8)&0x1F), byte(pmt1 & 0xFF),
		byte(prog2 >> 8), byte(prog2 & 0xFF),
		0xE0 | byte((pmt2>>8)&0x1F), byte(pmt2 & 0xFF),
		0x00, 0x00, 0x00, 0x00, // CRC placeholder
	}

	crc := CalculateMPEG2CRC32(patSection[:16])
	binary.BigEndian.PutUint32(patSection[16:20], crc)

	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 // PUSI=1, PID=0
	pkt[2] = 0x00
	pkt[3] = 0x10 // AFC=0x01
	pkt[4] = 0x00 // pointer field = 0
	copy(pkt[5:], patSection)
	return pkt
}

func createMultiPacketPMTWithVersion(pmtPID uint16, videoPID uint16, isHEVC bool, hasVideo bool, version uint8, currentNext uint8) [][]byte {
	streamType := byte(0x1B) // H.264
	if isHEVC {
		streamType = 0x24 // H.265
	}

	var pmtSection []byte
	if hasVideo {
		pmtSection = []byte{
			0x02,       // table_id = 2 (PMT)
			0xB0, 0x17, // section_syntax + length = 23 (3+23=26 bytes total)
			0x00, 0x01, // program_number = 1
			0xC0 | ((version & 0x1F) << 1) | (currentNext & 0x01), // version + current_next
			0x00, 0x00, // section 0 / last 0
			0xE0 | byte((videoPID>>8)&0x1F), byte(videoPID & 0xFF), // PCR PID = videoPID
			0xF0, 0x00, // program_info_length = 0

			// ES 1: Video
			streamType,
			0xE0 | byte((videoPID>>8)&0x1F), byte(videoPID & 0xFF),
			0xF0, 0x00, // ES info len = 0

			// ES 2: Audio (AC3 = 0x06)
			0x06,
			0xE0, 0x55, // Audio PID 85
			0xF0, 0x00,

			0x00, 0x00, 0x00, 0x00, // CRC32 placeholder
		}
	} else {
		// Audio-only PMT (no video stream)
		pmtSection = []byte{
			0x02,       // table_id = 2 (PMT)
			0xB0, 0x12, // section length = 18 (total 21 bytes)
			0x00, 0x01, // program_number = 1
			0xC0 | ((version & 0x1F) << 1) | (currentNext & 0x01),
			0x00, 0x00,
			0xE0, 0x55, // PCR PID = 85
			0xF0, 0x00,

			// ES 1: Audio only
			0x06,
			0xE0, 0x55,
			0xF0, 0x00,

			0x00, 0x00, 0x00, 0x00, // CRC32 placeholder
		}
	}

	crc := CalculateMPEG2CRC32(pmtSection[:len(pmtSection)-4])
	binary.BigEndian.PutUint32(pmtSection[len(pmtSection)-4:], crc)

	// Packet 1: PUSI=true, pointer_field=0, contains first 15 bytes
	pkt1 := make([]byte, TSPacketSize)
	pkt1[0] = SyncByte
	pkt1[1] = 0x40 | byte((pmtPID>>8)&0x1F)
	pkt1[2] = byte(pmtPID & 0xFF)
	pkt1[3] = 0x30   // AFC=0x03, CC=0
	pkt1[4] = 167    // AF length
	pkt1[5] = 0x00   // AF flags
	pkt1[172] = 0x00 // pointer_field = 0
	copy(pkt1[173:], pmtSection[:15])

	// Packet 2: PUSI=false, contains remaining bytes (CC=1)
	pkt2 := make([]byte, TSPacketSize)
	pkt2[0] = SyncByte
	pkt2[1] = byte((pmtPID >> 8) & 0x1F)
	pkt2[2] = byte(pmtPID & 0xFF)
	pkt2[3] = 0x11 // AFC=0x01, CC=1
	copy(pkt2[4:], pmtSection[15:])

	return [][]byte{pkt1, pkt2}
}

func createMultiPacketPMT(pmtPID uint16, videoPID uint16, isHEVC bool) [][]byte {
	return createMultiPacketPMTWithVersion(pmtPID, videoPID, isHEVC, true, 0, 1)
}

func TestMasterRing_MultiPacketPATPMT_Assembly(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256

	patPackets := createMultiPacketPAT(pmtPID)
	for _, pkt := range patPackets {
		if _, err := r.Push(pkt); err != nil {
			t.Fatalf("push pat failed: %v", err)
		}
	}

	pmtPackets := createMultiPacketPMT(pmtPID, videoPID, false)
	for _, pkt := range pmtPackets {
		if _, err := r.Push(pkt); err != nil {
			t.Fatalf("push pmt failed: %v", err)
		}
	}

	vPID, vCodec := r.VideoDetails()
	if vPID != videoPID || vCodec != CodecH264 {
		t.Fatalf("unexpected video details: vPID=%d, vCodec=%v", vPID, vCodec)
	}

	preamble := r.PATPMTPreamble()
	if len(preamble) != 4*TSPacketSize {
		t.Fatalf("expected preamble length %d, got %d", 4*TSPacketSize, len(preamble))
	}
	if !bytes.Equal(preamble[:2*TSPacketSize], append(patPackets[0], patPackets[1]...)) {
		t.Fatalf("PAT portion mismatch in preamble")
	}
	if !bytes.Equal(preamble[2*TSPacketSize:], append(pmtPackets[0], pmtPackets[1]...)) {
		t.Fatalf("PMT portion mismatch in preamble")
	}
}

func TestMasterRing_StatefulAnnexBSplitAcrossTSPackets(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		_, _ = r.Push(pkt)
	}

	frameStartOffset := r.Head()

	es1 := make([]byte, TSPacketSize-18)
	es1[len(es1)-2] = 0x00
	es1[len(es1)-1] = 0x00
	pkt1 := createVideoPESPacket(videoPID, true, 0, es1)

	es2 := make([]byte, 100)
	es2[0] = 0x01
	es2[1] = 0x05
	pkt2 := createBasicPacket(videoPID, false, 1)
	copy(pkt2[4:], es2)

	if _, err := r.Push(pkt1); err != nil {
		t.Fatalf("push pkt1 failed: %v", err)
	}
	if _, err := r.Push(pkt2); err != nil {
		t.Fatalf("push pkt2 failed: %v", err)
	}

	offset, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("expected keyframe to be indexed across split packet boundary")
	}
	if offset != frameStartOffset {
		t.Fatalf("keyframe indexed at wrong offset: expected %d, got %d", frameStartOffset, offset)
	}
}

func TestMasterRing_SPSWithoutIDR_NotIndexed(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		_, _ = r.Push(pkt)
	}

	// Standalone SPS parameter update (NAL 7) without PPS or slice
	es := []byte{
		0x00, 0x00, 0x00, 0x01, 0x07, 0x42, 0x00, 0x1E,
	}
	pkt := createVideoPESPacket(videoPID, true, 0, es)
	if _, err := r.Push(pkt); err != nil {
		t.Fatalf("push failed: %v", err)
	}

	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("SPS alone was incorrectly indexed as keyframe")
	}
}

func TestMasterRing_NonVideoPID_RandomAccessIndicatorIgnored(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256
	const audioPID = 85

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		_, _ = r.Push(pkt)
	}

	audioPkt := make([]byte, TSPacketSize)
	audioPkt[0] = SyncByte
	audioPkt[1] = byte((audioPID >> 8) & 0x1F)
	audioPkt[2] = byte(audioPID & 0xFF)
	audioPkt[3] = 0x30
	audioPkt[4] = 5
	audioPkt[5] = 0x40

	if _, err := r.Push(audioPkt); err != nil {
		t.Fatalf("push failed: %v", err)
	}

	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("audio packet with random_access_indicator was incorrectly indexed as keyframe")
	}
}

func TestMasterRing_HEVC_KeyframeDetection(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, true) {
		_, _ = r.Push(pkt)
	}

	vPID, vCodec := r.VideoDetails()
	if vPID != videoPID || vCodec != CodecH265 {
		t.Fatalf("expected HEVC codec, got %v", vCodec)
	}

	frameStartOffset := r.Head()

	es := []byte{
		0x00, 0x00, 0x00, 0x01, 0x40, 0x01,
		0x00, 0x00, 0x00, 0x01, 0x42, 0x01,
		0x00, 0x00, 0x00, 0x01, 0x44, 0x01,
		0x00, 0x00, 0x00, 0x01, 0x26, 0x01,
	}
	pkt := createVideoPESPacket(videoPID, true, 0, es)
	if _, err := r.Push(pkt); err != nil {
		t.Fatalf("push failed: %v", err)
	}

	offset, ok := r.LatestKeyframeOffset()
	if !ok || offset != frameStartOffset {
		t.Fatalf("HEVC IDR frame not indexed at PUSI offset: expected %d, got %d (ok=%v)", frameStartOffset, offset, ok)
	}
}

func TestMasterRing_PushLargerThanRingCapacity(t *testing.T) {
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	largeData := make([]byte, 50*TSPacketSize)
	for i := 0; i < 50; i++ {
		pkt := createBasicPacket(100, false, uint8(i%16))
		copy(largeData[i*TSPacketSize:], pkt)
	}

	n, err := r.Push(largeData)
	if err != nil || n != len(largeData) {
		t.Fatalf("large push failed: n=%d err=%v", n, err)
	}

	if r.BufferedBytes() != 10*TSPacketSize {
		t.Fatalf("expected buffered bytes %d, got %d", 10*TSPacketSize, r.BufferedBytes())
	}
	if r.Head() != 50*TSPacketSize {
		t.Fatalf("expected head %d, got %d", 50*TSPacketSize, r.Head())
	}
	if r.Tail() != 40*TSPacketSize {
		t.Fatalf("expected tail %d, got %d", 40*TSPacketSize, r.Tail())
	}
}

func TestMasterRing_KeyframePruneAllOlderThanTail(t *testing.T) {
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		_, _ = r.Push(pkt)
	}

	es := []byte{0x00, 0x00, 0x01, 0x05}
	_, _ = r.Push(createVideoPESPacket(videoPID, true, 0, es))

	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Fatalf("expected initial keyframe indexed")
	}

	for i := 0; i < 30; i++ {
		_, _ = r.Push(createBasicPacket(videoPID, false, uint8(i%16)))
	}

	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("expected keyframe older than tail to be pruned, but was still reported")
	}
}

func TestSubscriberReader_ConcurrentReadSeekClose_NoDeadlock(t *testing.T) {
	r := NewMasterRing(50 * TSPacketSize)
	defer r.Close()

	const numReaders = 20
	readers := make([]*SubscriberReader, numReaders)
	for i := 0; i < numReaders; i++ {
		readers[i] = r.NewSubscriberReader(0)
	}

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	go func() {
		cc := uint8(0)
		for {
			select {
			case <-stopChan:
				return
			default:
				pkt := createBasicPacket(256, false, cc)
				cc++
				_, _ = r.Push(pkt)
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reader := readers[idx]
			buf := make([]byte, TSPacketSize)

			for j := 0; j < 50; j++ {
				switch j % 5 {
				case 0, 1:
					_, _ = reader.Read(buf)
				case 2:
					_, _ = reader.SeekToLatestKeyframe()
				case 3:
					_ = reader.Offset()
					_ = reader.DroppedBytes()
				case 4:
					if j > 30 {
						_ = reader.Close()
					}
				}
			}
			_ = reader.Close()
		}(i)
	}

	wg.Wait()
	close(stopChan)
}

func TestMasterRing_MultiReaderConcurrency(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const numPackets = 500
	const numReaders = 10

	var allData []byte
	for i := 0; i < numPackets; i++ {
		pkt := createBasicPacket(256, false, uint8(i%16))
		allData = append(allData, pkt...)
	}

	readers := make([]*SubscriberReader, numReaders)
	for i := 0; i < numReaders; i++ {
		readers[i] = r.NewSubscriberReader(0)
	}

	var wg sync.WaitGroup
	readResults := make([][]byte, numReaders)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var buf bytes.Buffer
			tmp := make([]byte, TSPacketSize*5)

			for buf.Len() < len(allData) {
				n, err := readers[idx].Read(tmp)
				if err != nil {
					break
				}
				buf.Write(tmp[:n])
			}
			readResults[idx] = buf.Bytes()
		}(i)
	}

	go func() {
		chunkSize := TSPacketSize * 10
		for i := 0; i < len(allData); i += chunkSize {
			end := i + chunkSize
			if end > len(allData) {
				end = len(allData)
			}
			_, _ = r.Push(allData[i:end])
			time.Sleep(1 * time.Millisecond)
		}
		r.Close()
	}()

	wg.Wait()

	for i := 0; i < numReaders; i++ {
		if len(readResults[i]) != len(allData) {
			t.Fatalf("reader %d length mismatch: expected %d, got %d", i, len(allData), len(readResults[i]))
		}
		if !bytes.Equal(readResults[i], allData) {
			t.Fatalf("reader %d content mismatch", i)
		}
	}
}

// An overrun on a service with no video resumes at the tail: there are no random
// access points to wait for and no decoder state to corrupt. The topology has to be
// known for that to be decidable, so the PMT below is pushed before the subscriber
// attaches - a ring that has parsed no PMT waits instead, which is covered by
// TestSubscriberReader_UnknownTopologyWaitsRatherThanResumingAtTail.
func TestMasterRing_SlowSubscriberOverrunIsolation(t *testing.T) {
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	for _, pkt := range createMultiPacketPAT(100) {
		_, _ = r.Push(pkt)
	}
	for _, pkt := range createMultiPacketPMTWithVersion(100, 0, false, false, 0, 1) {
		_, _ = r.Push(pkt)
	}
	if r.facts.VideoPID != 0 {
		t.Fatalf("test setup: expected an audio-only PMT, got video PID %d", r.facts.VideoPID)
	}

	reader := r.NewSubscriberReader(0)
	defer reader.Close()

	baseline := r.Tail()
	for i := 0; i < 30; i++ {
		_, _ = r.Push(createBasicPacket(256, false, uint8(i%16)))
	}

	buf := make([]byte, TSPacketSize)
	n, err := reader.Read(buf)
	if err != nil || n != TSPacketSize {
		t.Fatalf("read after overrun failed: n=%d err=%v", n, err)
	}

	// Everything the ring evicted between the subscriber's start offset and the
	// tail it found on wake-up, and nothing else.
	if want := r.Tail() - baseline; reader.DroppedBytes() != want {
		t.Fatalf("expected %d dropped bytes, got %d", want, reader.DroppedBytes())
	}
	if got := reader.ResyncSkippedBytes(); got != 0 {
		t.Fatalf("audio-only recovery skipped %d bytes; there is no keyframe to skip to", got)
	}
}

func TestMasterRing_CloseWakesAllReaders(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)

	const numReaders = 5
	readers := make([]*SubscriberReader, numReaders)
	for i := 0; i < numReaders; i++ {
		readers[i] = r.NewSubscriberReader(0)
		defer readers[i].Close()
	}

	var wg sync.WaitGroup
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			buf := make([]byte, TSPacketSize)
			_, _ = readers[idx].Read(buf)
		}(i)
	}

	time.Sleep(20 * time.Millisecond)
	r.Close()
	wg.Wait()
}

func TestMasterRing_CCDiscontinuity_AbortsCorruptedSection(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	patPackets := createMultiPacketPAT(pmtPID)

	// 1. Send first packet with CC=0
	pkt1 := make([]byte, TSPacketSize)
	copy(pkt1, patPackets[0])
	pkt1[3] = (pkt1[3] & 0xF0) | 0x00
	_, _ = r.Push(pkt1)

	// 2. Send corrupted second packet with CC=5 (gap!)
	pkt2Corrupted := make([]byte, TSPacketSize)
	copy(pkt2Corrupted, patPackets[1])
	pkt2Corrupted[3] = (pkt2Corrupted[3] & 0xF0) | 0x05
	_, _ = r.Push(pkt2Corrupted)

	if r.facts.PMTPID != 0 {
		t.Fatalf("corrupted PAT section with CC gap was incorrectly accepted")
	}

	// 3. Send clean PAT packets (CC=6, CC=7)
	pkt1Valid := make([]byte, TSPacketSize)
	copy(pkt1Valid, patPackets[0])
	pkt1Valid[3] = (pkt1Valid[3] & 0xF0) | 0x06
	_, _ = r.Push(pkt1Valid)

	pkt2Valid := make([]byte, TSPacketSize)
	copy(pkt2Valid, patPackets[1])
	pkt2Valid[3] = (pkt2Valid[3] & 0xF0) | 0x07
	_, _ = r.Push(pkt2Valid)

	if r.facts.PMTPID != pmtPID {
		t.Fatalf("expected pmtPID=%d after clean retransmission, got %d", pmtPID, r.facts.PMTPID)
	}
}

func TestMasterRing_CurrentNextIndicator_InactiveTableIgnored(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	patPacketsInactive := createMultiPacketPATWithVersion(pmtPID, 0, 0)
	for _, pkt := range patPacketsInactive {
		_, _ = r.Push(pkt)
	}

	if r.facts.PMTPID != 0 {
		t.Fatalf("inactive PAT table (current_next=0) was incorrectly accepted")
	}

	for _, pkt := range createMultiPacketPATWithVersion(pmtPID, 0, 1) {
		_, _ = r.Push(pkt)
	}

	if r.facts.PMTPID != pmtPID {
		t.Fatalf("active PAT table was not accepted: got %d", r.facts.PMTPID)
	}
}

func TestMasterRing_PMTVersionChange_PurgesStaleKeyframes(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}

	for _, pkt := range createMultiPacketPMTWithVersion(pmtPID, 256, false, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	vPID1, vCodec1 := r.VideoDetails()
	if vPID1 != 256 || vCodec1 != CodecH264 {
		t.Fatalf("expected initial vPID=256 H.264, got %d %v", vPID1, vCodec1)
	}

	es1 := []byte{0x00, 0x00, 0x01, 0x05}
	_, _ = r.Push(createVideoPESPacket(256, true, 0, es1))

	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Fatalf("expected keyframe on PID 256 indexed")
	}

	pmtV2 := createMultiPacketPMTWithVersion(pmtPID, 512, true, true, 1, 1)
	for _, pkt := range pmtV2 {
		_, _ = r.Push(pkt)
	}

	vPID2, vCodec2 := r.VideoDetails()
	if vPID2 != 512 || vCodec2 != CodecH265 {
		t.Fatalf("expected dynamically updated vPID=512 H.265, got %d %v", vPID2, vCodec2)
	}

	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("stale keyframe from PID 256 was not purged after PMT version update!")
	}

	frameStart := r.Head()
	es2 := []byte{0x00, 0x00, 0x00, 0x01, 0x26, 0x01}
	_, _ = r.Push(createVideoPESPacket(512, true, 0, es2))

	newOffset, ok := r.LatestKeyframeOffset()
	if !ok || newOffset != frameStart {
		t.Fatalf("expected new keyframe indexed on PID 512 at %d, got %d (ok=%v)", frameStart, newOffset, ok)
	}
}

func TestMasterRing_PMTVersionChange_NoVideoStream_LeavesPIDZero(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}

	for _, pkt := range createMultiPacketPMTWithVersion(pmtPID, 256, false, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	if vPID, _ := r.VideoDetails(); vPID != 256 {
		t.Fatalf("expected initial vPID=256, got %d", vPID)
	}

	for _, pkt := range createMultiPacketPMTWithVersion(pmtPID, 0, false, false, 1, 1) {
		_, _ = r.Push(pkt)
	}

	vPID, vCodec := r.VideoDetails()
	if vPID != 0 || vCodec != CodecUnknown {
		t.Fatalf("expected vPID=0 and CodecUnknown after audio-only PMT update, got %d %v", vPID, vCodec)
	}
}

func TestMasterRing_CRC32_CorruptedSectionRejected(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	patPackets := createMultiPacketPAT(pmtPID)

	pkt2Corrupt := make([]byte, TSPacketSize)
	copy(pkt2Corrupt, patPackets[1])
	pkt2Corrupt[6] ^= 0xFF

	_, _ = r.Push(patPackets[0])
	_, _ = r.Push(pkt2Corrupt)

	if r.facts.PMTPID != 0 {
		t.Fatalf("PAT section with corrupted CRC32 was incorrectly accepted")
	}

	_, _ = r.Push(patPackets[0])
	_, _ = r.Push(patPackets[1])

	if r.facts.PMTPID != pmtPID {
		t.Fatalf("valid PAT section was not accepted: got %d", r.facts.PMTPID)
	}
}

func TestMasterRing_TargetProgramNumber_Selection(t *testing.T) {
	multiPAT := createMultiProgramPAT(1, 100, 2, 200)

	r1 := NewMasterRing(100 * TSPacketSize)
	defer r1.Close()
	_, _ = r1.Push(multiPAT)

	if r1.facts.PMTPID != 100 {
		t.Fatalf("expected default r1 to select PMT 100, got %d", r1.facts.PMTPID)
	}

	r2 := NewMasterRingWithProgram(100*TSPacketSize, 2)
	defer r2.Close()
	_, _ = r2.Push(multiPAT)

	if r2.facts.PMTPID != 200 {
		t.Fatalf("expected r2 to select PMT 200 for program 2, got %d", r2.facts.PMTPID)
	}
}

func TestMasterRing_RepeatedSamePMT_DoesNotPurgeKeyframes(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256

	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	pmtV0 := createMultiPacketPMTWithVersion(pmtPID, videoPID, false, true, 0, 1)
	for _, pkt := range pmtV0 {
		_, _ = r.Push(pkt)
	}

	frameStart := r.Head()
	es := []byte{0x00, 0x00, 0x01, 0x05}
	_, _ = r.Push(createVideoPESPacket(videoPID, true, 0, es))

	offset1, ok := r.LatestKeyframeOffset()
	if !ok || offset1 != frameStart {
		t.Fatalf("expected keyframe indexed at %d, got %d (ok=%v)", frameStart, offset1, ok)
	}

	for _, pkt := range pmtV0 {
		_, _ = r.Push(pkt)
	}

	offset2, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("keyframe was incorrectly purged by identical PMT re-transmission!")
	}
	if offset2 != offset1 {
		t.Fatalf("keyframe offset changed: expected %d, got %d", offset1, offset2)
	}
}

func TestMasterRing_SetTargetProgram_InvalidatesOldProgramState(t *testing.T) {
	multiPAT := createMultiProgramPAT(1, 100, 2, 200)

	r := NewMasterRingWithProgram(100*TSPacketSize, 1)
	defer r.Close()

	_, _ = r.Push(multiPAT)
	if r.facts.PMTPID != 100 {
		t.Fatalf("expected pmtPID=100 for program 1, got %d", r.facts.PMTPID)
	}

	for _, pkt := range createMultiPacketPMTWithVersion(100, 256, false, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	_, _ = r.Push(createVideoPESPacket(256, true, 0, []byte{0x00, 0x00, 0x01, 0x05}))
	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Fatalf("expected initial keyframe indexed")
	}

	r.SetTargetProgram(2)

	if vPID, _ := r.VideoDetails(); vPID != 0 {
		t.Fatalf("expected vPID=0 immediately after SetTargetProgram, got %d", vPID)
	}
	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("expected keyframe purged immediately after SetTargetProgram")
	}

	_, _ = r.Push(multiPAT)
	if r.facts.PMTPID != 200 {
		t.Fatalf("expected pmtPID=200 after resolving program 2, got %d", r.facts.PMTPID)
	}

	for _, pkt := range createMultiPacketPMTWithVersion(200, 512, true, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	vPID2, vCodec2 := r.VideoDetails()
	if vPID2 != 512 || vCodec2 != CodecH265 {
		t.Fatalf("expected vPID=512 HEVC for program 2, got %d %v", vPID2, vCodec2)
	}
}

// --- NEW BROADCAST RE-AUDIT SCENARIOS ---

// 1. Pointer field > 0 completes previous section and starts new section in the same TS packet
func TestMasterRing_PointerField_CompletesPreviousSectionAndStartsNew(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	// Section 1: Program 1 -> PMT 100 (16 bytes total)
	sec1 := createPATSectionBytes(1, 100, 0, 1, 0, 0)
	// Section 2: Program 2 -> PMT 200 (16 bytes total)
	sec2 := createPATSectionBytes(2, 200, 1, 1, 0, 0)

	// Packet 1 (PUSI=1, CC=0): starts Section 1 with first 10 bytes (6 bytes remaining)
	pkt1 := make([]byte, TSPacketSize)
	pkt1[0] = SyncByte
	pkt1[1] = 0x40
	pkt1[2] = 0x00
	pkt1[3] = 0x30 // AF + payload, CC=0
	pkt1[4] = 172  // AF len
	pkt1[5] = 0x00
	pkt1[177] = 0x00 // pointer_field = 0
	copy(pkt1[178:], sec1[:10])

	_, _ = r.Push(pkt1)
	if r.facts.PMTPID != 0 {
		t.Fatalf("section 1 should be incomplete")
	}

	// Packet 2 (PUSI=1, CC=1): pointer_field = 6!
	// Bytes 5..10: remaining 6 bytes of Section 1
	// Bytes 11..26: full Section 2!
	pkt2 := make([]byte, TSPacketSize)
	pkt2[0] = SyncByte
	pkt2[1] = 0x40 // PUSI=1
	pkt2[2] = 0x00
	pkt2[3] = 0x11            // payload only, CC=1
	pkt2[4] = 6               // pointer_field = 6
	copy(pkt2[5:], sec1[10:]) // 6 bytes completing Section 1
	copy(pkt2[11:], sec2)     // 16 bytes containing Section 2
	for i := 27; i < TSPacketSize; i++ {
		pkt2[i] = 0xFF // Stuffing
	}

	_, _ = r.Push(pkt2)

	// Section 2 was also parsed in the same packet and updated PMT PID to 200!
	if r.facts.PMTPID != 200 {
		t.Fatalf("expected pmtPID=200 from Section 2 parsed in same packet, got %d", r.facts.PMTPID)
	}
}

// 2. Multiple complete sections packed into a single TS payload
func TestMasterRing_MultipleCompleteSectionsInSinglePacket(t *testing.T) {
	r := NewMasterRingWithProgram(100*TSPacketSize, 20)
	defer r.Close()

	// Section 0: Program 10 -> PMT 100 (16 bytes)
	sec0 := createPATSectionBytes(10, 100, 3, 1, 0, 1)
	// Section 1: Program 20 -> PMT 200 (16 bytes)
	sec1 := createPATSectionBytes(20, 200, 3, 1, 1, 1)

	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 // PUSI=1, PID=0
	pkt[2] = 0x00
	pkt[3] = 0x10 // AFC=0x01
	pkt[4] = 0x00 // pointer_field = 0
	copy(pkt[5:], sec0)
	copy(pkt[21:], sec1)
	for i := 37; i < TSPacketSize; i++ {
		pkt[i] = 0xFF // Stuffing
	}

	_, _ = r.Push(pkt)

	if r.facts.PMTPID != 200 {
		t.Fatalf("expected pmtPID=200 for target program 20 from multi-section packet, got %d", r.facts.PMTPID)
	}
}

// 3. Multi-section PAT (Section 0/1 + Section 1/1) where target is only in Section 1
func TestMasterRing_MultiSectionPAT_TargetOnlyInSection1(t *testing.T) {
	r := NewMasterRingWithProgram(100*TSPacketSize, 30)
	defer r.Close()

	sec0 := createPATSectionBytes(10, 100, 3, 1, 0, 1)
	sec1 := createPATSectionBytes(30, 300, 3, 1, 1, 1)

	pkt0 := make([]byte, TSPacketSize)
	pkt0[0] = SyncByte
	pkt0[1] = 0x40
	pkt0[2] = 0x00
	pkt0[3] = 0x10 // CC=0
	pkt0[4] = 0x00
	copy(pkt0[5:], sec0)

	pkt1 := make([]byte, TSPacketSize)
	pkt1[0] = SyncByte
	pkt1[1] = 0x40
	pkt1[2] = 0x00
	pkt1[3] = 0x11 // CC=1
	pkt1[4] = 0x00
	copy(pkt1[5:], sec1)

	// Push Section 0 (target 30 not in Section 0)
	_, _ = r.Push(pkt0)
	if r.facts.PMTPID != 0 {
		t.Fatalf("table should not be activated before section 1 arrives")
	}

	// Push Section 1
	_, _ = r.Push(pkt1)
	if r.facts.PMTPID != 300 {
		t.Fatalf("expected pmtPID=300 from Section 1, got %d", r.facts.PMTPID)
	}

	// Verify PAT preamble contains both Section 0 and Section 1 TS packets
	preamble := r.PATPMTPreamble()
	if len(preamble) < 2*TSPacketSize {
		t.Fatalf("preamble must contain at least 2 PAT packets, got %d", len(preamble))
	}
	if !bytes.Equal(preamble[:TSPacketSize], pkt0) || !bytes.Equal(preamble[TSPacketSize:2*TSPacketSize], pkt1) {
		t.Fatalf("preamble must contain both Section 0 and Section 1 packets")
	}
}

// 4. Repeated carousel arrival of Section 0 does not destroy multi-section preamble or target resolution
func TestMasterRing_CarouselRepeatedSection0_PreservesMultiSectionPreamble(t *testing.T) {
	r := NewMasterRingWithProgram(100*TSPacketSize, 30)
	defer r.Close()

	sec0 := createPATSectionBytes(10, 100, 3, 1, 0, 1)
	sec1 := createPATSectionBytes(30, 300, 3, 1, 1, 1)

	pkt0 := make([]byte, TSPacketSize)
	pkt0[0] = SyncByte
	pkt0[1] = 0x40
	pkt0[2] = 0x00
	pkt0[3] = 0x10 // CC=0
	pkt0[4] = 0x00
	copy(pkt0[5:], sec0)

	pkt1 := make([]byte, TSPacketSize)
	pkt1[0] = SyncByte
	pkt1[1] = 0x40
	pkt1[2] = 0x00
	pkt1[3] = 0x11 // CC=1
	pkt1[4] = 0x00
	copy(pkt1[5:], sec1)

	_, _ = r.Push(pkt0)
	_, _ = r.Push(pkt1)

	if r.facts.PMTPID != 300 {
		t.Fatalf("expected pmtPID=300, got %d", r.facts.PMTPID)
	}

	// Periodic DVB Carousel: Section 0 arrives again!
	pkt0Next := make([]byte, TSPacketSize)
	copy(pkt0Next, pkt0)
	pkt0Next[3] = 0x12 // CC=2
	_, _ = r.Push(pkt0Next)

	// Target resolution must NOT be destroyed
	if r.facts.PMTPID != 300 {
		t.Fatalf("expected pmtPID=300 preserved after carousel section 0, got %d", r.facts.PMTPID)
	}

	// Preamble must STILL contain both sections
	preamble := r.PATPMTPreamble()
	if len(preamble) != 2*TSPacketSize {
		t.Fatalf("preamble must preserve both sections (length %d), got %d", 2*TSPacketSize, len(preamble))
	}
}

// 5. Missing section prevents table activation on new version
func TestMasterRing_MissingSection_PreventsTableActivation(t *testing.T) {
	r := NewMasterRingWithProgram(100*TSPacketSize, 30)
	defer r.Close()

	// Initial table v0 (single section) -> pmtPID = 100
	secInit := createPATSectionBytes(30, 100, 0, 1, 0, 0)
	pktInit := make([]byte, TSPacketSize)
	pktInit[0] = SyncByte
	pktInit[1] = 0x40
	pktInit[2] = 0x00
	pktInit[3] = 0x10
	pktInit[4] = 0x00
	copy(pktInit[5:], secInit)
	_, _ = r.Push(pktInit)

	if r.facts.PMTPID != 100 {
		t.Fatalf("expected initial pmtPID=100, got %d", r.facts.PMTPID)
	}

	// New version v1 announces last_section_number = 1 (2 sections required)
	// Push only Section 0 of v1 (which does NOT have program 30)
	secV1_0 := createPATSectionBytes(10, 500, 1, 1, 0, 1)
	pktV1_0 := make([]byte, TSPacketSize)
	pktV1_0[0] = SyncByte
	pktV1_0[1] = 0x40
	pktV1_0[2] = 0x00
	pktV1_0[3] = 0x11
	pktV1_0[4] = 0x00
	copy(pktV1_0[5:], secV1_0)
	_, _ = r.Push(pktV1_0)

	// Because Section 1 of v1 is missing, table v1 is INCOMPLETE and must not activate!
	if r.facts.PMTPID != 100 {
		t.Fatalf("incomplete table version must not activate: expected pmtPID=100, got %d", r.facts.PMTPID)
	}

	// Push Section 1 of v1 (with program 30 -> PMT 300)
	secV1_1 := createPATSectionBytes(30, 300, 1, 1, 1, 1)
	pktV1_1 := make([]byte, TSPacketSize)
	pktV1_1[0] = SyncByte
	pktV1_1[1] = 0x40
	pktV1_1[2] = 0x00
	pktV1_1[3] = 0x12
	pktV1_1[4] = 0x00
	copy(pktV1_1[5:], secV1_1)
	_, _ = r.Push(pktV1_1)

	// Now table v1 is complete and activates!
	if r.facts.PMTPID != 300 {
		t.Fatalf("expected pmtPID=300 after complete v1 assembly, got %d", r.facts.PMTPID)
	}
}

// 6. Duplicate CC packet is ignored silently without discontinuity reset
func TestMasterRing_DuplicateCC_IgnoredWithoutDiscontinuity(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	patPackets := createMultiPacketPAT(pmtPID)

	// 1. Push Packet 1 with CC=0
	_, _ = r.Push(patPackets[0])

	// 2. Push exact duplicate of Packet 1 with same CC=0 (DVB retransmission)
	_, _ = r.Push(patPackets[0])

	// 3. Push Packet 2 with CC=1
	_, _ = r.Push(patPackets[1])

	// Assembly must have succeeded seamlessly
	if r.facts.PMTPID != pmtPID {
		t.Fatalf("duplicate CC packet caused incorrect discontinuity reset: got pmtPID=%d", r.facts.PMTPID)
	}
}

// 7. Fragmented 1-2 byte PSI section header split across TS packets
func TestMasterRing_PSIHeaderSplitAcrossPackets(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	sec := createPATSectionBytes(1, pmtPID, 0, 1, 0, 0) // 16 bytes total

	// Packet 1: ends with only 2 bytes of the section header!
	// AF length = 188 - 4 - 1 (pointer) - 2 (header bytes) = 181
	pkt1 := make([]byte, TSPacketSize)
	pkt1[0] = SyncByte
	pkt1[1] = 0x40 // PUSI=1
	pkt1[2] = 0x00
	pkt1[3] = 0x30   // AF + payload, CC=0
	pkt1[4] = 180    // AF length
	pkt1[5] = 0x00   // AF flags
	pkt1[185] = 0x00 // pointer_field = 0
	pkt1[186] = sec[0]
	pkt1[187] = sec[1]

	// Packet 2: PUSI=0, starts with remaining header byte sec[2] and the rest of the body (sec[3..15])
	pkt2 := make([]byte, TSPacketSize)
	pkt2[0] = SyncByte
	pkt2[1] = 0x00 // PUSI=0
	pkt2[2] = 0x00
	pkt2[3] = 0x11 // CC=1
	copy(pkt2[4:], sec[2:])

	_, _ = r.Push(pkt1)
	if r.facts.PMTPID != 0 {
		t.Fatalf("table should not be resolved after 2 header bytes")
	}

	_, _ = r.Push(pkt2)
	if r.facts.PMTPID != pmtPID {
		t.Fatalf("expected pmtPID=%d from section with split header across packets, got %d", pmtPID, r.facts.PMTPID)
	}
}

// 8. Same CC with different payload triggers discontinuity reset
func TestMasterRing_SameCCDifferentPayload_ResetsAssembly(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	patPackets := createMultiPacketPAT(pmtPID)

	// 1. Push Packet 1 (PUSI=1, CC=0)
	_, _ = r.Push(patPackets[0])

	// 2. Push glitch continuation packet with SAME CC=0 but PUSI=0 and different payload
	corruptedPkt := make([]byte, TSPacketSize)
	corruptedPkt[0] = SyncByte
	corruptedPkt[1] = 0x00 // PUSI=0
	corruptedPkt[2] = 0x00
	corruptedPkt[3] = 0x10 // CC=0 (same CC as pkt 1, but completely different packet!)
	_, _ = r.Push(corruptedPkt)

	// 3. Push Packet 2 (PUSI=0, CC=1)
	_, _ = r.Push(patPackets[1])

	// Assembly must have been aborted by the glitch packet
	if r.facts.PMTPID != 0 {
		t.Fatalf("same CC with different payload failed to abort corrupted assembly")
	}
}

// 9. Multiple complete sections in same packet: preamble does not duplicate raw packet
func TestMasterRing_MultipleSectionsSamePacket_PreambleDoesNotDuplicatePacket(t *testing.T) {
	r := NewMasterRingWithProgram(100*TSPacketSize, 20)
	defer r.Close()

	sec0 := createPATSectionBytes(10, 100, 3, 1, 0, 1)
	sec1 := createPATSectionBytes(20, 200, 3, 1, 1, 1)

	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 // PUSI=1, PID=0
	pkt[2] = 0x00
	pkt[3] = 0x10 // AFC=0x01
	pkt[4] = 0x00 // pointer_field = 0
	copy(pkt[5:], sec0)
	copy(pkt[21:], sec1)
	for i := 37; i < TSPacketSize; i++ {
		pkt[i] = 0xFF // Stuffing
	}

	_, _ = r.Push(pkt)

	if r.facts.PMTPID != 200 {
		t.Fatalf("expected pmtPID=200, got %d", r.facts.PMTPID)
	}

	// Preamble must contain the packet EXACTLY ONCE (188 bytes), not duplicated (376 bytes)
	preamble := r.PATPMTPreamble()
	if len(preamble) != TSPacketSize {
		t.Fatalf("expected deduplicated preamble length %d, got %d", TSPacketSize, len(preamble))
	}
}

func TestMasterRing_MPEG2_SequenceHeaderAndIFrame(t *testing.T) {
	r := NewMasterRingWithProgram(100*TSPacketSize, 1)
	defer r.Close()

	// 1. PAT with prog 1 -> PMT PID 100
	for _, pkt := range createMultiPacketPAT(100) {
		_, _ = r.Push(pkt)
	}

	// 2. PMT with video stream_type=0x02 (MPEG-2 Video) on PID 200
	pmtSection := []byte{
		0x02,       // table_id = 2 (PMT)
		0xB0, 0x17, // section_syntax + length = 23 (26 bytes total)
		0x00, 0x01, // program_number = 1
		0xC1,       // version 0, current_next 1
		0x00, 0x00, // section 0 / last 0
		0xE0, 0xC8, // PCR PID = 200
		0xF0, 0x00, // program_info_length = 0

		// ES 1: MPEG-2 Video (0x02) on PID 200
		0x02,
		0xE0, 0xC8,
		0xF0, 0x00,

		// ES 2: MP2 Audio (0x03) on PID 201
		0x03,
		0xE0, 0xC9,
		0xF0, 0x00,

		0x00, 0x00, 0x00, 0x00, // CRC32 placeholder
	}
	crc := CalculateMPEG2CRC32(pmtSection[:22])
	binary.BigEndian.PutUint32(pmtSection[22:26], crc)

	pmtPkt := make([]byte, TSPacketSize)
	pmtPkt[0] = SyncByte
	pmtPkt[1] = 0x40 | byte((100>>8)&0x1F)
	pmtPkt[2] = byte(100 & 0xFF)
	pmtPkt[3] = 0x10 // AFC=0x01
	pmtPkt[4] = 0x00 // pointer_field = 0
	copy(pmtPkt[5:], pmtSection)
	for i := 5 + len(pmtSection); i < TSPacketSize; i++ {
		pmtPkt[i] = 0xFF
	}
	_, _ = r.Push(pmtPkt)

	if r.facts.VideoCodec != CodecMPEG2 {
		t.Fatalf("expected CodecMPEG2, got %v", r.facts.VideoCodec)
	}

	// 3. MPEG-2 Video PES containing Sequence Header (00 00 01 B3) and I-Frame Picture Header (00 00 01 00 00 08)
	// Picture header: temporal_ref=0 (10 bits), picture_coding_type=1 (bits 5..3 of byte 1 -> 0x08)
	mpeg2ES := []byte{
		0x00, 0x00, 0x01, 0xB3, 0x2D, 0x02, 0x40, 0x23, // Sequence Header (Codec Config)
		0x00, 0x00, 0x01, 0xB8, 0x00, 0x08, 0x00, 0x00, // GOP Header
		0x00, 0x00, 0x01, 0x00, 0x00, 0x08, 0x00, 0x00, // Picture Header (I-Frame: coding_type=1)
	}

	videoPkt := createVideoPESPacket(200, true, 0, mpeg2ES)
	_, _ = r.Push(videoPkt)

	// Finalize the access unit with another packet
	nextAUPkt := createVideoPESPacket(200, true, 1, []byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x10}) // P-Frame
	_, _ = r.Push(nextAUPkt)

	kf, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("expected valid keyframe offset for MPEG-2 I-Frame, got ok=false")
	}
	if kf < 0 {
		t.Fatalf("expected non-negative keyframe offset, got %d", kf)
	}
}
