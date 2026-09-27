// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"context"
	"io"
	"time"
)

const (
	tsPacketSize = 188
	syncByte     = 0x47

	pidPAT   = 0x0000
	pidPMT   = 0x1000
	pidVideo = 0x0100
	pidAudio = 0x0101
)

// mpeg2CRC32 calculates the 32-bit CRC used in MPEG-2 / DVB PSI tables.
func mpeg2CRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		for i := 0; i < 8; i++ {
			bit := ((b >> (7 - i)) & 1) == 1
			c15 := ((crc >> 31) & 1) == 1
			crc <<= 1
			if c15 != bit {
				crc ^= 0x04C11DB7
			}
		}
	}
	return crc
}

// GeneratePATPacket builds a standard 188-byte PAT MPEG-TS packet.
func GeneratePATPacket(cc uint8) []byte {
	pkt := make([]byte, tsPacketSize)
	for i := range pkt {
		pkt[i] = 0xFF
	}

	// TS Header: Sync(0x47), PUSI(1), PID(0x0000), PayloadOnly(0x10), CC
	pkt[0] = syncByte
	pkt[1] = 0x40 // PUSI = 1, PID = 0
	pkt[2] = 0x00
	pkt[3] = 0x10 | (cc & 0x0F)

	// Pointer field
	pkt[4] = 0x00

	// Table ID: 0x00 (PAT)
	section := []byte{
		0x00,       // Table ID
		0xB0, 0x0D, // section_syntax_indicator=1, section_length=13 (9 + 4 CRC)
		0x00, 0x01, // transport_stream_id = 1
		0xC1,       // version=0, current_next=1
		0x00,       // section_number = 0
		0x00,       // last_section_number = 0
		0x00, 0x01, // program_number = 1
		0xE0 | (pidPMT >> 8), uint8(pidPMT & 0xFF), // PMT PID = 0x1000
	}

	crc := mpeg2CRC32(section)
	section = append(section,
		byte(crc>>24),
		byte(crc>>16),
		byte(crc>>8),
		byte(crc),
	)

	copy(pkt[5:], section)
	return pkt
}

// GeneratePMTPacket builds a standard 188-byte PMT MPEG-TS packet.
func GeneratePMTPacket(cc uint8) []byte {
	pkt := make([]byte, tsPacketSize)
	for i := range pkt {
		pkt[i] = 0xFF
	}

	// TS Header: Sync(0x47), PUSI(1), PID(0x1000), PayloadOnly(0x10), CC
	pkt[0] = syncByte
	pkt[1] = 0x40 | uint8(pidPMT>>8)
	pkt[2] = uint8(pidPMT & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F)

	// Pointer field
	pkt[4] = 0x00

	// PMT Section:
	// Stream 1: 0x1B (H.264 video) on pidVideo (0x0100)
	// Stream 2: 0x0F (AAC audio) on pidAudio (0x0101)
	section := []byte{
		0x02,       // Table ID (PMT)
		0xB0, 0x17, // section_syntax_indicator=1, section_length=23 (19 + 4 CRC)
		0x00, 0x01, // program_number = 1
		0xC1,                                           // version=0, current_next=1
		0x00,                                           // section_number = 0
		0x00,                                           // last_section_number = 0
		0xE0 | (pidVideo >> 8), uint8(pidVideo & 0xFF), // PCR_PID = 0x0100
		0xF0, 0x00, // program_info_length = 0

		// Stream 1: Video H.264
		0x1B,
		0xE0 | (pidVideo >> 8), uint8(pidVideo & 0xFF),
		0xF0, 0x00,

		// Stream 2: Audio AAC
		0x0F,
		0xE0 | (pidAudio >> 8), uint8(pidAudio & 0xFF),
		0xF0, 0x00,
	}

	crc := mpeg2CRC32(section)
	section = append(section,
		byte(crc>>24),
		byte(crc>>16),
		byte(crc>>8),
		byte(crc),
	)

	copy(pkt[5:], section)
	return pkt
}

// GenerateVideoPacket builds an 188-byte Video PES MPEG-TS packet.
func GenerateVideoPacket(cc uint8, pcrBase uint64, scrambled bool) []byte {
	pkt := make([]byte, tsPacketSize)

	pkt[0] = syncByte
	pkt[1] = 0x40 | uint8(pidVideo>>8) // PUSI = 1
	pkt[2] = uint8(pidVideo & 0xFF)

	scrambleBits := uint8(0x00)
	if scrambled {
		scrambleBits = 0x80 // Scrambled with even key
	}

	// Adaptation + Payload (0x30)
	pkt[3] = scrambleBits | 0x30 | (cc & 0x0F)

	// Adaptation Field: length 7, PCR present (0x50 = PCR + Random Access)
	pkt[4] = 7
	pkt[5] = 0x50

	// Write 33-bit PCR base into 6 bytes
	pkt[6] = byte(pcrBase >> 25)
	pkt[7] = byte(pcrBase >> 17)
	pkt[8] = byte(pcrBase >> 9)
	pkt[9] = byte(pcrBase >> 1)
	pkt[10] = byte((pcrBase & 1) << 7)
	pkt[11] = 0x00 // PCR ext = 0

	// PES Header (starts at offset 12)
	// Start code: 0x00, 0x00, 0x01, 0xE0 (Video Stream 0)
	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00, // Unbounded length
		0x80, 0x80, 0x05, // PTS present
		// PTS = 90kHz timestamp derived from pcrBase
		byte(0x21 | ((pcrBase >> 29) & 0x0E)),
		byte(pcrBase >> 22),
		byte(0x01 | ((pcrBase >> 14) & 0xFE)),
		byte(pcrBase >> 7),
		byte(0x01 | ((pcrBase << 1) & 0xFE)),
		// NAL AUD (Access Unit Delimiter) + NAL IDR Slice header
		0x00, 0x00, 0x00, 0x01, 0x09, 0xF0,
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88,
	}

	copy(pkt[12:], pesHeader)
	// Fill rest with video payload bytes
	for i := 12 + len(pesHeader); i < tsPacketSize; i++ {
		pkt[i] = byte(i)
	}
	return pkt
}

// GenerateAudioPacket builds an 188-byte Audio PES MPEG-TS packet.
func GenerateAudioPacket(cc uint8, pts uint64, scrambled bool) []byte {
	pkt := make([]byte, tsPacketSize)

	pkt[0] = syncByte
	pkt[1] = 0x40 | uint8(pidAudio>>8) // PUSI = 1
	pkt[2] = uint8(pidAudio & 0xFF)

	scrambleBits := uint8(0x00)
	if scrambled {
		scrambleBits = 0x80
	}

	// Payload only (0x10)
	pkt[3] = scrambleBits | 0x10 | (cc & 0x0F)

	// PES Header (offset 4)
	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xC0, // Audio Stream 0
		0x00, 0xB2, // Length = 178 (184-byte payload - 6-byte PES prefix)
		0x80, 0x80, 0x05, // PTS present
		byte(0x21 | ((pts >> 29) & 0x0E)),
		byte(pts >> 22),
		byte(0x01 | ((pts >> 14) & 0xFE)),
		byte(pts >> 7),
		byte(0x01 | ((pts << 1) & 0xFE)),
		// ADTS Header dummy (AAC syncword 0xFFF)
		0xFF, 0xF1, 0x50, 0x80, 0x03, 0x7F, 0xFC,
	}

	copy(pkt[4:], pesHeader)
	for i := 4 + len(pesHeader); i < tsPacketSize; i++ {
		pkt[i] = byte(i ^ 0xAA)
	}
	return pkt
}

// StreamTSBatch writes a batch of valid TS packets (PAT, PMT, Video, Audio) to w.
func StreamTSBatch(w io.Writer, pcrBase *uint64, cc *uint8, scrambled bool) error {
	pat := GeneratePATPacket(*cc)
	*cc = (*cc + 1) & 0x0F
	pmt := GeneratePMTPacket(*cc)
	*cc = (*cc + 1) & 0x0F
	vid := GenerateVideoPacket(*cc, *pcrBase, scrambled)
	*cc = (*cc + 1) & 0x0F
	aud := GenerateAudioPacket(*cc, *pcrBase, scrambled)
	*cc = (*cc + 1) & 0x0F

	*pcrBase += 3600 // Advance clock by 40ms (25 fps)

	batch := make([]byte, 0, tsPacketSize*4)
	batch = append(batch, pat...)
	batch = append(batch, pmt...)
	batch = append(batch, vid...)
	batch = append(batch, aud...)

	_, err := w.Write(batch)
	return err
}

// StreamTSContinuously streams TS batches until ctx is cancelled or maxBatches is reached (0 = infinite).
func StreamTSContinuously(ctx context.Context, w io.Writer, maxBatches int, camGraceWindow time.Duration) error {
	startedAt := time.Now()
	var pcrBase uint64 = 90000 // 1 sec at 90 kHz
	var cc uint8 = 0
	batches := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if maxBatches > 0 && batches >= maxBatches {
			return nil
		}

		scrambled := false
		if camGraceWindow > 0 && time.Since(startedAt) < camGraceWindow {
			scrambled = true
		}

		if err := StreamTSBatch(w, &pcrBase, &cc, scrambled); err != nil {
			return err
		}
		batches++

		// Small yield/cadence to avoid pegging CPU if streaming in tests
		time.Sleep(1 * time.Millisecond)
	}
}
