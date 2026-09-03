// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"testing"
)

// Observed behaviour of the Go PSI reference that has NOT been adjudicated.
//
// These are deliberately not in the shared corpus. The corpus is what a second
// implementation is held to, and holding one to a behaviour nobody has decided
// is right means the second implementation gets written to reproduce a defect
// on purpose - which is the specific failure this whole migration is shaped to
// avoid.
//
// So each test here pins what the reference does today, states the question it
// raises, and says what would have to happen before it becomes corpus material.
// A red test here means the behaviour changed, which is either the fix landing
// or a regression; either way it needs a person, not a re-record.
//
// Every one of these was found by reading the parser during the step 5a
// inventory and then proved here. None of them is a hypothesis.

// --- 1. auto-select over a multi-section PAT ------------------------------

// With no target program the core takes "the first program in the PAT". Over a
// PAT that spans several sections it does not: the inner loop breaks out of the
// section, but the outer loop only stops early when a target was named, so each
// later section overwrites the choice and the last one wins.
//
//	for sIdx := 0; sIdx <= lastSectionNum; sIdx++ {
//	        ... matchedPID = progPID; break            // first program of THIS section
//	        if matchedPID > 0 && c.targetProgramNumber > 0 { break }   // never taken when target == 0
//	}
//
// Question: should an unspecified target follow the first program of the table,
// or the first program of the last section? The first is what the code says it
// does. Nothing in xg2g calls the core with target 0 in production - the session
// layer always names a service - so this has no live consequence today.
func TestObserved_AutoSelectOverAMultiSectionPATTakesTheLastSection(t *testing.T) {
	sec0 := patSection(psiTSID, 0, 0, 1, 1, patProgram{3, psiPMTPID3})
	sec1 := patSection(psiTSID, 0, 1, 1, 1, patProgram{5, psiPMTPID2})

	c := NewGoCore(0)
	res, err := c.Ingest(context.Background(), 0,
		chunkOf(flatten(psiPackets(0, 0, 0, sec0), psiPackets(0, 1, 0, sec1))...))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if got, want := res.Facts.PMTPID, uint16(psiPMTPID2); got != want {
		t.Fatalf("auto-select picked PMT PID 0x%04X, and this test records that it picks 0x%04X "+
			"(the first program of the LAST section) rather than 0x%04X (the first program of the table). "+
			"If this changed on purpose, move the case into the corpus; if not, it is a regression.",
			got, want, uint16(psiPMTPID3))
	}
}

// --- 2. stream type 0x87 -------------------------------------------------

// A/52 Annex A and the ATSC registration give 0x81 to AC-3 and 0x87 to E-AC-3.
// AudioCodecFromStreamType maps both to "ac3".
//
// Question: should a 0x87 stream be reported as "eac3"? The codec string reaches
// the client capability check and the ffmpeg audio mapping, so this is not only
// cosmetic - but the observer that reads the elementary stream detects E-AC-3
// from bsid regardless of the label, and every DVB service measured here carries
// E-AC-3 as stream type 0x06 with an E-AC-3 descriptor, which is mapped
// correctly. 0x87 is the ATSC shape, which this deployment does not receive.
func TestObserved_StreamType0x87IsReportedAsAC3(t *testing.T) {
	if got := AudioCodecFromStreamType(0x87, nil); got != "ac3" {
		t.Fatalf("stream type 0x87 now reports %q; this test recorded %q. "+
			"ATSC registers 0x87 as E-AC-3, so %q may be the intended fix - decide it, do not re-record it.",
			got, "ac3", "eac3")
	}
	if got := AudioCodecFromStreamType(0x81, nil); got != "ac3" {
		t.Fatalf("stream type 0x81 reports %q, want %q", got, "ac3")
	}
}

// --- 3. an ES_info_length that reaches past the ES loop -------------------

// The elementary stream walk bounds its entries with esEnd (the section minus
// its CRC) but bounds the descriptor slice with len(sData):
//
//	esEnd := len(sData) - 4
//	for i := esStart; i+5 <= esEnd && i < len(sData); {
//	        if dStart := i + 5; dStart+esInfoLen <= len(sData) {
//	                descriptors = sData[dStart : dStart+esInfoLen]
//
// So an entry that ends exactly at esEnd while declaring up to four bytes of
// descriptors gets the section's own CRC handed to it as its descriptor block.
// Four bytes is the whole overread - the CRC is where the section ends - but
// four bytes is enough: hasAudioDescriptor accepts a tag it knows with a
// declared length of 2 or less, and that is what decides whether a private-data
// stream is treated as audio at all.
//
// This test searches for a section whose CRC happens to start with an AC-3
// descriptor tag, and shows the resulting phantom track.
//
// Question: should the descriptor slice be bounded by esEnd? Reading a section's
// CRC as its own content is wrong under any reading of 13818-1. The reason it is
// here rather than in the corpus is that the fix changes a reported fact, and
// changing the reference while Rust is being written against it is exactly what
// the migration rules forbid. It needs its own PR.
func TestObserved_ADeclaredDescriptorLengthCanReachIntoTheSectionCRC(t *testing.T) {
	for filler := 0; filler < 100000; filler++ {
		pi := []byte{0x09, 0x03, byte(filler >> 16), byte(filler >> 8), byte(filler)}
		sec := pmtSection(1, psiVideoPID, 0, 0, 0, 1, pi,
			esH264(psiVideoPID),
			esAC3(psiAudioPID1, descAC3(0x02)),
		)
		// Drop the four descriptor bytes, leave ES_info_length claiming them, and
		// shrink section_length so the section is otherwise well formed.
		s := append(cloneSlice(sec[:len(sec)-8]), 0, 0, 0, 0)
		newSecLen := len(s) - 3
		s[1] = 0xB0 | byte((newSecLen>>8)&0x0F)
		s[2] = byte(newSecLen)
		crc := psiCRC32(s[:len(s)-4])
		s[len(s)-4], s[len(s)-3] = byte(crc>>24), byte(crc>>16)
		s[len(s)-2], s[len(s)-1] = byte(crc>>8), byte(crc)

		// The four CRC bytes are read as a descriptor block. hasAudioDescriptor
		// takes a tag it knows whose declared length fits inside those bytes.
		switch byte(crc >> 24) {
		case descriptorAC3, descriptorEnhAC3, descriptorDTS, descriptorAAC, descriptorDTSHD:
		default:
			continue
		}
		if byte(crc>>16) > 2 {
			continue
		}

		c := NewGoCore(1)
		ctx := context.Background()
		pat := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1})
		if _, err := c.Ingest(ctx, 0, chunkOf(psiPackets(0, 0, 0, pat)...)); err != nil {
			t.Fatalf("ingest PAT: %v", err)
		}
		res, err := c.Ingest(ctx, TSPacketSize, chunkOf(psiPackets(psiPMTPID1, 0, 0, s)...))
		if err != nil {
			t.Fatalf("ingest PMT: %v", err)
		}

		if len(res.Facts.AudioPIDs) != 1 || res.Facts.AudioPIDs[0] != psiAudioPID1 {
			t.Fatalf("filler %d, CRC %08X: expected the overread to produce exactly one phantom "+
				"audio track on PID %d, got %v. If the descriptor slice is now bounded by esEnd "+
				"this test has served its purpose and the behaviour belongs in the corpus.",
				filler, crc, uint16(psiAudioPID1), res.Facts.AudioPIDs)
		}
		t.Logf("section CRC %08X was read as descriptor tag 0x%02X length %d, "+
			"which made PID %d an %q track that the PMT never declared",
			crc, byte(crc>>24), byte(crc>>16), res.Facts.AudioPIDs[0], res.Facts.AudioTracks[0].Codec)
		return
	}
	t.Fatal("no section CRC in 100000 tries started with a descriptor tag this parser accepts; " +
		"either the CRC or the section layout changed, and this test no longer proves what it claims")
}

// --- 4. table_id on the assembler path ------------------------------------

// The table_id is checked where a section header is scanned whole inside one
// payload:
//
//	tableID := payload[offset]
//	if tableID != expectedTableID { break }
//
// A section whose first one or two bytes land at the very end of a packet does
// not reach that line. It is stashed by the branch above it, completed by the
// assembler on the next packet, and handed to processCompletePSISectionLocked,
// which validates length, CRC and current_next - but never the table_id.
//
// So a section of any table, arriving split in that particular way with a valid
// CRC, is parsed as whatever table its PID is being watched for. Below, an SDT
// section moves the program to a different PMT PID.
//
// Question: should the completed section's table_id be checked before it is
// interpreted? A PID carrying two tables is not exotic, and the CRC does not
// discriminate between them - it only says the bytes are intact.
func TestObserved_ASectionHeaderSplitAcrossPacketsSkipsTheTableIDCheck(t *testing.T) {
	// 364 bytes: 183 fit the first packet, leaving 181 for a pointer field, which
	// puts the following section's start exactly 2 bytes from the payload end.
	programs := []patProgram{{1, psiPMTPID1}}
	for i := 0; i < 87; i++ {
		programs = append(programs, patProgram{number: uint16(100 + i), pid: uint16(0x0400 + i)})
	}
	sectionA := patSection(psiTSID, 0, 0, 0, 1, programs...)
	if len(sectionA) != 364 {
		t.Fatalf("section A is %d bytes, the case needs 364", len(sectionA))
	}

	fake := patSection(psiTSID, 1, 0, 0, 1, patProgram{1, psiPMTPID2})
	fake[0] = 0x42 // service_description_section
	crc := psiCRC32(fake[:len(fake)-4])
	fake[len(fake)-4], fake[len(fake)-3] = byte(crc>>24), byte(crc>>16)
	fake[len(fake)-2], fake[len(fake)-1] = byte(crc>>8), byte(crc)

	payload1 := append([]byte{181}, sectionA[183:]...)
	payload1 = append(payload1, fake[:2]...)
	if len(payload1) != TSPacketSize-4 {
		t.Fatalf("payload 1 is %d bytes, the case needs %d", len(payload1), TSPacketSize-4)
	}

	c := NewGoCore(1)
	res, err := c.Ingest(context.Background(), 0, chunkOf(
		psiPackets(0, 0, 0, sectionA)[0],
		psiPacketRaw(0, true, 1, payload1),
		psiPacketRaw(0, false, 2, fake[2:]),
	))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if got := res.Facts.PMTPID; got != uint16(psiPMTPID2) {
		t.Fatalf("PMT PID is 0x%04X; this test records that a table_id 0x42 section split across "+
			"the packet boundary is accepted as a PAT and moves it to 0x%04X. Rejecting it "+
			"(0x%04X, what section A said) would be the fix - decide it, do not re-record it.",
			got, uint16(psiPMTPID2), uint16(psiPMTPID1))
	}
}

// --- 5. Reset clears what SetTargetProgram does not ------------------------

// Reset is not part of the Core interface, so it is not corpus material, but it
// is the other half of the stale-fact finding recorded in the corpus case
// a_target_switch_leaves_the_old_program_number_and_version_behind: Reset
// rebuilds the whole core and so clears pmtProgramNumber and pmtVersion, while
// SetTargetProgram clears neither. Whatever is decided for one has to be decided
// for the other, and this test is where the asymmetry is visible.
func TestObserved_ResetClearsTheProgramNumberThatSetTargetProgramKeeps(t *testing.T) {
	pat := patSection(psiTSID, 0, 0, 0, 1, patProgram{1, psiPMTPID1}, patProgram{2, psiPMTPID2})
	pmt := pmtSection(1, psiVideoPID, 9, 0, 0, 1, nil, esH264(psiVideoPID))

	build := func() *GoCore {
		c := NewGoCore(1)
		if _, err := c.Ingest(context.Background(), 0,
			chunkOf(flatten(psiPackets(0, 0, 0, pat), psiPackets(psiPMTPID1, 0, 0, pmt))...)); err != nil {
			t.Fatalf("ingest: %v", err)
		}
		return c
	}

	switched, err := build().SetTargetProgram(context.Background(), 2)
	if err != nil {
		t.Fatalf("set target: %v", err)
	}
	if switched.Facts.ProgramNumber != 1 || switched.Facts.PMTVersion != 9 {
		t.Fatalf("SetTargetProgram now reports programNumber=%d pmtVersion=%d; this test recorded "+
			"1 and 9 surviving the switch. If they are cleared now, update the corpus case too.",
			switched.Facts.ProgramNumber, switched.Facts.PMTVersion)
	}

	reset := build().Reset()
	if reset.Facts.ProgramNumber != 0 || reset.Facts.PMTVersion != 0 {
		t.Fatalf("Reset left programNumber=%d pmtVersion=%d, want both 0",
			reset.Facts.ProgramNumber, reset.Facts.PMTVersion)
	}
}
