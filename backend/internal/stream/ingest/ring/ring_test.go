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
		// PES Header: 00 00 01 E0, len=0 (unbounded), flags=0x80 (PTS present), headerDataLen=5, PTS
		pkt[4] = 0x00
		pkt[5] = 0x00
		pkt[6] = 0x01
		pkt[7] = 0xE0 // Video stream ID
		pkt[8] = 0x00
		pkt[9] = 0x00
		pkt[10] = 0x80
		pkt[11] = 0x80
		pkt[12] = 0x05 // PES header data len = 5 (PTS)
		pkt[13] = 0x21 // PTS byte 1
		pkt[14] = 0x00
		pkt[15] = 0x01
		pkt[16] = 0x00
		pkt[17] = 0x01
		payloadOffset = 18
	}

	copy(pkt[payloadOffset:], esPayload)
	return pkt
}

func createMultiPacketPATWithVersion(pmtPID uint16, version uint8, currentNext uint8) [][]byte {
	// Build a PAT section spanning across 2 TS packets
	// Section length: 13 bytes (total section size = 16 bytes)
	patSection := []byte{
		0x00,       // table_id = 0 (PAT) [0]
		0xB0, 0x0D, // section_syntax + length = 13 [1, 2]
		0x00, 0x01, // transport_stream_id = 1 [3, 4]
		0xC0 | ((version & 0x1F) << 1) | (currentNext & 0x01), // version + current_next [5]
		0x00,       // section_number = 0 [6]
		0x00,       // last_section_number = 0 [7]
		0x00, 0x01, // program_number = 1 [8, 9]
		0xE0 | byte((pmtPID>>8)&0x1F), byte(pmtPID & 0xFF), // PMT PID [10, 11]
		0x00, 0x00, 0x00, 0x00, // CRC32 placeholder [12..15]
	}

	// Calculate and write exact MPEG-2 CRC32
	crc := CalculateMPEG2CRC32(patSection[:12])
	binary.BigEndian.PutUint32(patSection[12:16], crc)

	// Packet 1: PUSI=true, pointer_field=0, contains first 10 bytes of section + AF
	pkt1 := make([]byte, TSPacketSize)
	pkt1[0] = SyncByte
	pkt1[1] = 0x40 // PUSI=1, PID=0
	pkt1[2] = 0x00
	pkt1[3] = 0x30   // AFC=0x03 (AF + payload), CC=0
	pkt1[4] = 172    // Adaptation field length = 172
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

	// Verify authoritative video PID and Codec parsed from PMT
	vPID, vCodec := r.VideoDetails()
	if vPID != videoPID || vCodec != CodecH264 {
		t.Fatalf("unexpected video details: vPID=%d, vCodec=%v", vPID, vCodec)
	}

	// Verify PATPMTPreamble contains all 4 raw TS packets (2 PAT + 2 PMT = 752 bytes)
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

	// Setup PAT & PMT
	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, false) {
		_, _ = r.Push(pkt)
	}

	// Head offset before video frame
	frameStartOffset := r.Head()

	// Packet 1: Video PES start (PUSI=1). ES payload ends with 00 00 at the very end of TS packet!
	es1 := make([]byte, TSPacketSize-18)
	es1[len(es1)-2] = 0x00
	es1[len(es1)-1] = 0x00
	pkt1 := createVideoPESPacket(videoPID, true, 0, es1)

	// Packet 2: Continuation (PUSI=0). Starts with 01 05 (startcode suffix + NAL type 5 IDR slice)!
	es2 := make([]byte, 100)
	es2[0] = 0x01
	es2[1] = 0x05 // IDR slice NAL unit
	pkt2 := createBasicPacket(videoPID, false, 1)
	copy(pkt2[4:], es2)

	if _, err := r.Push(pkt1); err != nil {
		t.Fatalf("push pkt1 failed: %v", err)
	}
	if _, err := r.Push(pkt2); err != nil {
		t.Fatalf("push pkt2 failed: %v", err)
	}

	// Must have indexed the keyframe at the exact PES PUSI start offset!
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

	// Video PES with SPS (NAL 7) and non-IDR slice (NAL 1), but NO IDR (NAL 5)
	es := []byte{
		0x00, 0x00, 0x00, 0x01, 0x07, 0x42, 0x00, 0x1E, // SPS
		0x00, 0x00, 0x00, 0x01, 0x08, 0xCE, 0x3C, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x01, 0x9A, 0x00, 0x00, // Non-IDR Slice (NAL 1)
	}
	pkt := createVideoPESPacket(videoPID, true, 0, es)
	if _, err := r.Push(pkt); err != nil {
		t.Fatalf("push failed: %v", err)
	}

	// SPS without IDR must NOT be indexed as keyframe!
	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("SPS without IDR was incorrectly indexed as keyframe")
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

	// Audio packet on PID 85 with random_access_indicator = 1
	audioPkt := make([]byte, TSPacketSize)
	audioPkt[0] = SyncByte
	audioPkt[1] = byte((audioPID >> 8) & 0x1F)
	audioPkt[2] = byte(audioPID & 0xFF)
	audioPkt[3] = 0x30 // AF + payload
	audioPkt[4] = 5
	audioPkt[5] = 0x40 // random_access_indicator = 1

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
	for _, pkt := range createMultiPacketPMT(pmtPID, videoPID, true) { // HEVC PMT
		_, _ = r.Push(pkt)
	}

	vPID, vCodec := r.VideoDetails()
	if vPID != videoPID || vCodec != CodecH265 {
		t.Fatalf("expected HEVC codec, got %v", vCodec)
	}

	frameStartOffset := r.Head()

	// HEVC PES packet with VPS (32), SPS (33), PPS (34), and IDR_W_RADL (19 -> byte = (19<<1) = 0x26)
	es := []byte{
		0x00, 0x00, 0x00, 0x01, 0x40, 0x01, // VPS (32)
		0x00, 0x00, 0x00, 0x01, 0x42, 0x01, // SPS (33)
		0x00, 0x00, 0x00, 0x01, 0x44, 0x01, // PPS (34)
		0x00, 0x00, 0x00, 0x01, 0x26, 0x01, // IDR_W_RADL (19)
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
	// Small ring: 10 packets = 1880 bytes
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	// Push 50 packets (9,400 bytes, 5x ring capacity) in a single Push call
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
	// Ring capacity: 10 packets
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

	// Push IDR frame at offset X
	es := []byte{0x00, 0x00, 0x01, 0x05}
	_, _ = r.Push(createVideoPESPacket(videoPID, true, 0, es))

	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Fatalf("expected initial keyframe indexed")
	}

	// Overwrite ring completely by pushing 30 non-keyframe packets
	for i := 0; i < 30; i++ {
		_, _ = r.Push(createBasicPacket(videoPID, false, uint8(i%16)))
	}

	// Keyframe is now older than tail: must be pruned and return false
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

	// Start continuous writer
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

	// 20 concurrent goroutines racing Read, Seek, Offset, DroppedBytes, Close
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

	// Push in chunks
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

func TestMasterRing_SlowSubscriberOverrunIsolation(t *testing.T) {
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	reader := r.NewSubscriberReader(0)
	defer reader.Close()

	// Push 30 packets (overflowing ring)
	for i := 0; i < 30; i++ {
		_, _ = r.Push(createBasicPacket(256, false, uint8(i%16)))
	}

	buf := make([]byte, TSPacketSize)
	n, err := reader.Read(buf)
	if err != nil || n != TSPacketSize {
		t.Fatalf("read after overrun failed: n=%d err=%v", n, err)
	}

	if reader.DroppedBytes() != 20*TSPacketSize {
		t.Fatalf("expected 3760 dropped bytes, got %d", reader.DroppedBytes())
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

	// 2. Send corrupted second packet with CC=5 (gap instead of CC=1!)
	pkt2Corrupted := make([]byte, TSPacketSize)
	copy(pkt2Corrupted, patPackets[1])
	pkt2Corrupted[3] = (pkt2Corrupted[3] & 0xF0) | 0x05
	_, _ = r.Push(pkt2Corrupted)

	// PMT PID must NOT be resolved because section assembly aborted due to CC gap
	if r.pmtPID != 0 {
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

	// Now it must be successfully resolved
	if r.pmtPID != pmtPID {
		t.Fatalf("expected pmtPID=%d after clean retransmission, got %d", pmtPID, r.pmtPID)
	}
}

func TestMasterRing_CurrentNextIndicator_InactiveTableIgnored(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	// Create PAT with current_next_indicator = 0
	patPacketsInactive := createMultiPacketPATWithVersion(pmtPID, 0, 0)
	for _, pkt := range patPacketsInactive {
		_, _ = r.Push(pkt)
	}

	if r.pmtPID != 0 {
		t.Fatalf("inactive PAT table (current_next=0) was incorrectly accepted")
	}

	// Send active table (current_next = 1)
	for _, pkt := range createMultiPacketPATWithVersion(pmtPID, 0, 1) {
		_, _ = r.Push(pkt)
	}

	if r.pmtPID != pmtPID {
		t.Fatalf("active PAT table was not accepted: got %d", r.pmtPID)
	}
}

func TestMasterRing_PMTVersionChange_PurgesStaleKeyframes(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}

	// 1. Initial PMT v1: Video PID 256 (H.264), Version 0
	for _, pkt := range createMultiPacketPMTWithVersion(pmtPID, 256, false, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	vPID1, vCodec1 := r.VideoDetails()
	if vPID1 != 256 || vCodec1 != CodecH264 {
		t.Fatalf("expected initial vPID=256 H.264, got %d %v", vPID1, vCodec1)
	}

	// Push Keyframe on PID 256
	es1 := []byte{0x00, 0x00, 0x01, 0x05}
	_, _ = r.Push(createVideoPESPacket(256, true, 0, es1))

	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Fatalf("expected keyframe on PID 256 indexed")
	}

	// 2. Updated PMT v2 on same PMT PID: Video PID 512 (H.265), Version 1
	pmtV2 := createMultiPacketPMTWithVersion(pmtPID, 512, true, true, 1, 1)
	for _, pkt := range pmtV2 {
		_, _ = r.Push(pkt)
	}

	vPID2, vCodec2 := r.VideoDetails()
	if vPID2 != 512 || vCodec2 != CodecH265 {
		t.Fatalf("expected dynamically updated vPID=512 H.265, got %d %v", vPID2, vCodec2)
	}

	// CRITICAL PROOF: Before first IDR on PID 512 arrives, stale keyframe from PID 256 MUST be purged!
	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("stale keyframe from PID 256 was not purged after PMT version update!")
	}

	// 3. Push Keyframe on PID 512
	frameStart := r.Head()
	es2 := []byte{0x00, 0x00, 0x00, 0x01, 0x26, 0x01} // HEVC IDR (NAL 19)
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

	// 1. Initial PMT v1: Video PID 256
	for _, pkt := range createMultiPacketPMTWithVersion(pmtPID, 256, false, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	if vPID, _ := r.VideoDetails(); vPID != 256 {
		t.Fatalf("expected initial vPID=256, got %d", vPID)
	}

	// 2. Updated PMT v2: Audio only (hasVideo = false), Version 1
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

	// Corrupt CRC32 in the second packet (last 4 bytes of section)
	pkt2Corrupt := make([]byte, TSPacketSize)
	copy(pkt2Corrupt, patPackets[1])
	pkt2Corrupt[6] ^= 0xFF // Flip bits in CRC

	_, _ = r.Push(patPackets[0])
	_, _ = r.Push(pkt2Corrupt)

	if r.pmtPID != 0 {
		t.Fatalf("PAT section with corrupted CRC32 was incorrectly accepted")
	}

	// Send valid PAT with correct CRC32
	_, _ = r.Push(patPackets[0])
	_, _ = r.Push(patPackets[1])

	if r.pmtPID != pmtPID {
		t.Fatalf("valid PAT section was not accepted: got %d", r.pmtPID)
	}
}

func TestMasterRing_TargetProgramNumber_Selection(t *testing.T) {
	// Program 1 -> PMT 100, Program 2 -> PMT 200
	multiPAT := createMultiProgramPAT(1, 100, 2, 200)

	// Ring 1 targeting default / program 1
	r1 := NewMasterRing(100 * TSPacketSize)
	defer r1.Close()
	_, _ = r1.Push(multiPAT)

	if r1.pmtPID != 100 {
		t.Fatalf("expected default r1 to select PMT 100, got %d", r1.pmtPID)
	}

	// Ring 2 targeting program 2
	r2 := NewMasterRingWithProgram(100*TSPacketSize, 2)
	defer r2.Close()
	_, _ = r2.Push(multiPAT)

	if r2.pmtPID != 200 {
		t.Fatalf("expected r2 to select PMT 200 for program 2, got %d", r2.pmtPID)
	}
}

func TestMasterRing_RepeatedSamePMT_DoesNotPurgeKeyframes(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	const pmtPID = 100
	const videoPID = 256

	// 1. Initial PAT + PMT v0
	for _, pkt := range createMultiPacketPAT(pmtPID) {
		_, _ = r.Push(pkt)
	}
	pmtV0 := createMultiPacketPMTWithVersion(pmtPID, videoPID, false, true, 0, 1)
	for _, pkt := range pmtV0 {
		_, _ = r.Push(pkt)
	}

	// 2. Push IDR Keyframe on video PID 256
	frameStart := r.Head()
	es := []byte{0x00, 0x00, 0x01, 0x05}
	_, _ = r.Push(createVideoPESPacket(videoPID, true, 0, es))

	offset1, ok := r.LatestKeyframeOffset()
	if !ok || offset1 != frameStart {
		t.Fatalf("expected keyframe indexed at %d, got %d (ok=%v)", frameStart, offset1, ok)
	}

	// 3. Periodic DVB re-transmission: push IDENTICAL PMT v0 again!
	for _, pkt := range pmtV0 {
		_, _ = r.Push(pkt)
	}

	// CRITICAL PROOF: Identical PMT re-transmission MUST NOT purge keyframes!
	offset2, ok := r.LatestKeyframeOffset()
	if !ok {
		t.Fatalf("keyframe was incorrectly purged by identical PMT re-transmission!")
	}
	if offset2 != offset1 {
		t.Fatalf("keyframe offset changed: expected %d, got %d", offset1, offset2)
	}
}

func TestMasterRing_SetTargetProgram_InvalidatesOldProgramState(t *testing.T) {
	// Program 1 -> PMT 100 (Video 256), Program 2 -> PMT 200 (Video 512)
	multiPAT := createMultiProgramPAT(1, 100, 2, 200)

	r := NewMasterRingWithProgram(100*TSPacketSize, 1)
	defer r.Close()

	_, _ = r.Push(multiPAT)
	if r.pmtPID != 100 {
		t.Fatalf("expected pmtPID=100 for program 1, got %d", r.pmtPID)
	}

	// Push PMT for program 1
	for _, pkt := range createMultiPacketPMTWithVersion(100, 256, false, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	// Push Keyframe on PID 256
	_, _ = r.Push(createVideoPESPacket(256, true, 0, []byte{0x00, 0x00, 0x01, 0x05}))
	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Fatalf("expected initial keyframe indexed")
	}

	// Change target program to program 2
	r.SetTargetProgram(2)

	// Old program and video state MUST be invalidated immediately
	if vPID, _ := r.VideoDetails(); vPID != 0 {
		t.Fatalf("expected vPID=0 immediately after SetTargetProgram, got %d", vPID)
	}
	if _, ok := r.LatestKeyframeOffset(); ok {
		t.Fatalf("expected keyframe purged immediately after SetTargetProgram")
	}

	// Next PAT push resolves PMT 200 for program 2
	_, _ = r.Push(multiPAT)
	if r.pmtPID != 200 {
		t.Fatalf("expected pmtPID=200 after resolving program 2, got %d", r.pmtPID)
	}

	// Push PMT for program 2
	for _, pkt := range createMultiPacketPMTWithVersion(200, 512, true, true, 0, 1) {
		_, _ = r.Push(pkt)
	}

	vPID2, vCodec2 := r.VideoDetails()
	if vPID2 != 512 || vCodec2 != CodecH265 {
		t.Fatalf("expected vPID=512 HEVC for program 2, got %d %v", vPID2, vCodec2)
	}
}
