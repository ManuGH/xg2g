// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func createTestPacket(pid uint16, cc uint8, isKeyframe bool) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = byte((pid >> 8) & 0x1F)
	pkt[2] = byte(pid & 0xFF)

	if isKeyframe {
		pkt[3] = 0x30 | (cc & 0x0F) // AFC=0x03 (AF+payload)
		pkt[4] = 7                  // AF length
		pkt[5] = 0x40               // random_access_indicator = 1
		pkt[6] = 0x00               // filler
		pkt[7] = 0x00
		pkt[8] = 0x00
		pkt[9] = 0x00
		pkt[10] = 0x00
		pkt[11] = 0x00
	} else {
		pkt[3] = 0x10 | (cc & 0x0F) // AFC=0x01 (payload only)
	}

	for i := 12; i < TSPacketSize; i++ {
		pkt[i] = byte(i)
	}
	return pkt
}

func createTestPATPacket(programNumber uint16, pmtPID uint16) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 // payload unit start indicator = 1, PID = 0
	pkt[2] = 0x00
	pkt[3] = 0x10 // AFC=0x01
	pkt[4] = 0x00 // pointer field = 0

	// PAT table
	table := pkt[5:]
	table[0] = 0x00 // table_id = PAT
	// section_length = 13 (syntax + id + ver + sec + last + prog loop (4) + crc (4))
	table[1] = 0xB0
	table[2] = 0x0D
	table[3] = 0x00 // transport_stream_id
	table[4] = 0x01
	table[5] = 0xC1 // version + current_next
	table[6] = 0x00 // section_number
	table[7] = 0x00 // last_section_number

	// Program 1 -> PMT PID
	table[8] = byte(programNumber >> 8)
	table[9] = byte(programNumber & 0xFF)
	table[10] = 0xE0 | byte((pmtPID>>8)&0x1F)
	table[11] = byte(pmtPID & 0xFF)

	return pkt
}

func createTestPMTPacket(pmtPID uint16) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	pkt[1] = 0x40 | byte((pmtPID>>8)&0x1F)
	pkt[2] = byte(pmtPID & 0xFF)
	pkt[3] = 0x10
	pkt[4] = 0x00

	table := pkt[5:]
	table[0] = 0x02 // table_id = PMT
	table[1] = 0xB0
	table[2] = 0x12 // section length

	return pkt
}

func TestMasterRing_MultiReaderConcurrency(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize) // ~18.8 KiB
	defer r.Close()

	const numPackets = 500
	const numReaders = 10

	var allData []byte
	for i := 0; i < numPackets; i++ {
		pkt := createTestPacket(100, uint8(i%16), (i%50) == 0)
		allData = append(allData, pkt...)
	}

	readers := make([]*SubscriberReader, numReaders)
	for i := 0; i < numReaders; i++ {
		readers[i] = r.NewSubscriberReader(0)
	}

	var wg sync.WaitGroup

	// Start 10 readers
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
					if errors.Is(err, io.EOF) {
						break
					}
					t.Errorf("reader %d error: %v", idx, err)
					return
				}
				buf.Write(tmp[:n])
			}
			readResults[idx] = buf.Bytes()
		}(i)
	}

	// Push data in chunks
	go func() {
		chunkSize := TSPacketSize * 10
		for i := 0; i < len(allData); i += chunkSize {
			end := i + chunkSize
			if end > len(allData) {
				end = len(allData)
			}
			_, _ = r.Push(allData[i:end])
			time.Sleep(2 * time.Millisecond)
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

func TestMasterRing_PATPMTPreamble(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	pat := createTestPATPacket(1, 256)
	pmt := createTestPMTPacket(256)
	videoPkt := createTestPacket(512, 0, true)

	if _, err := r.Push(pat); err != nil {
		t.Fatalf("push pat failed: %v", err)
	}
	if _, err := r.Push(pmt); err != nil {
		t.Fatalf("push pmt failed: %v", err)
	}
	if _, err := r.Push(videoPkt); err != nil {
		t.Fatalf("push video failed: %v", err)
	}

	preamble := r.PATPMTPreamble()
	if len(preamble) != TSPacketSize*2 {
		t.Fatalf("expected preamble length %d, got %d", TSPacketSize*2, len(preamble))
	}

	if !bytes.Equal(preamble[:TSPacketSize], pat) {
		t.Fatalf("PAT portion mismatch")
	}
	if !bytes.Equal(preamble[TSPacketSize:], pmt) {
		t.Fatalf("PMT portion mismatch")
	}
}

func TestMasterRing_KeyframeIndexingAndSeek(t *testing.T) {
	r := NewMasterRing(100 * TSPacketSize)
	defer r.Close()

	// 1. Push 5 normal packets
	for i := 0; i < 5; i++ {
		_, _ = r.Push(createTestPacket(100, uint8(i), false))
	}

	// 2. Push Keyframe 1 at byte offset 5 * 188 = 940
	kf1 := createTestPacket(100, 5, true)
	_, _ = r.Push(kf1)

	// 3. Push 3 normal packets
	for i := 6; i < 9; i++ {
		_, _ = r.Push(createTestPacket(100, uint8(i), false))
	}

	// 4. Push Keyframe 2 at byte offset 9 * 188 = 1692
	kf2 := createTestPacket(100, 9, true)
	_, _ = r.Push(kf2)

	latestOffset, ok := r.LatestKeyframeOffset()
	if !ok || latestOffset != 9*TSPacketSize {
		t.Fatalf("expected latest keyframe at %d, got %d (ok=%v)", 9*TSPacketSize, latestOffset, ok)
	}

	// Create a reader and seek to latest keyframe
	reader := r.NewSubscriberReader(0)
	defer reader.Close()

	seekOffset, err := reader.SeekToLatestKeyframe()
	if err != nil {
		t.Fatalf("seek failed: %v", err)
	}
	if seekOffset != latestOffset {
		t.Fatalf("seek offset mismatch: expected %d, got %d", latestOffset, seekOffset)
	}

	// Read first packet after seek: must be kf2!
	buf := make([]byte, TSPacketSize)
	n, err := reader.Read(buf)
	if err != nil || n != TSPacketSize {
		t.Fatalf("read after seek failed: n=%d err=%v", n, err)
	}
	if !bytes.Equal(buf, kf2) {
		t.Fatalf("read packet mismatch after keyframe seek")
	}
}

func TestMasterRing_SlowSubscriberOverrunIsolation(t *testing.T) {
	// Ring capacity: 10 packets = 1880 bytes
	r := NewMasterRing(10 * TSPacketSize)
	defer r.Close()

	reader := r.NewSubscriberReader(0)
	defer reader.Close()

	// Push 30 packets into the ring (buffer overflows 2 times over)
	for i := 0; i < 30; i++ {
		_, _ = r.Push(createTestPacket(100, uint8(i%16), false))
	}

	// Reader was at offset 0. Ring tail is now 20 * 188 = 3760 bytes.
	buf := make([]byte, TSPacketSize)
	n, err := reader.Read(buf)
	if err != nil || n != TSPacketSize {
		t.Fatalf("read after overrun failed: n=%d err=%v", n, err)
	}

	// Dropped bytes must be recorded and >= 20 packets (3760 bytes)
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
	errs := make([]error, numReaders)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			buf := make([]byte, TSPacketSize)
			_, err := readers[idx].Read(buf)
			errs[idx] = err
		}(i)
	}

	time.Sleep(30 * time.Millisecond)
	r.Close()

	wg.Wait()

	for i := 0; i < numReaders; i++ {
		if !errors.Is(errs[i], io.EOF) {
			t.Fatalf("reader %d expected io.EOF on close, got %v", i, errs[i])
		}
	}
}
