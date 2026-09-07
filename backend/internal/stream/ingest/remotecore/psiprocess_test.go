// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// Fixtures for the adversaries the shared corpus cannot carry: a table at the
// largest a section may be, and a table using every section number it may.
// Both are too large to write into the corpus file, and both are exactly the
// shapes R8 settled - so they are built here and sent through the real process.
//
// These build sections; they do not read them. Nothing here interprets PSI.

const tsPacket = mediafacts.TSPacketSize

func psiCRC(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

type program struct{ number, pid uint16 }

// patSection builds one PAT section carrying the given programmes.
func patSection(version, sectionNum, lastSectionNum uint8, programs ...program) []byte {
	length := 5 + 4*len(programs) + 4
	s := []byte{
		0x00,
		0xB0 | byte((length>>8)&0x0F), byte(length & 0xFF),
		0x00, 0x01, // transport stream id
		0xC1 | (version << 1), // reserved, version, current
		sectionNum, lastSectionNum,
	}
	for _, p := range programs {
		s = append(s, byte(p.number>>8), byte(p.number),
			0xE0|byte((p.pid>>8)&0x1F), byte(p.pid))
	}
	crc := psiCRC(s)
	return append(s, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

// psiPackets carries a section in as few packets as it fits into.
func psiPackets(pid uint16, startCC uint8, section []byte) [][]byte {
	body := append([]byte{0x00}, section...)
	var out [][]byte
	cc := startCC
	pusi := true
	for len(body) > 0 {
		n := tsPacket - 4
		if n > len(body) {
			n = len(body)
		}
		pkt := make([]byte, tsPacket)
		pkt[0] = 0x47
		pkt[1] = byte((pid >> 8) & 0x1F)
		if pusi {
			pkt[1] |= 0x40
		}
		pkt[2] = byte(pid)
		pkt[3] = 0x10 | (cc & 0x0F)
		copy(pkt[4:], body[:n])
		for i := 4 + n; i < tsPacket; i++ {
			pkt[i] = 0xFF
		}
		out = append(out, pkt)
		body = body[n:]
		cc = (cc + 1) & 0x0F
		pusi = false
	}
	return out
}

// starvedPackets carries a section one payload byte per packet, behind an
// adaptation field that eats the rest. Legal, and the cheapest way to ask a
// parser to hold a lot of memory for very little table.
//
// The first packet carries two payload bytes - the pointer field and the
// section's first byte - because a payload unit start carrying only a pointer
// field begins no section.
func starvedPackets(pid uint16, startCC uint8, section []byte) [][]byte {
	payloads := [][]byte{{0x00, section[0]}}
	for _, b := range section[1:] {
		payloads = append(payloads, []byte{b})
	}
	out := make([][]byte, 0, len(payloads))
	cc := startCC
	for i, payload := range payloads {
		pkt := make([]byte, tsPacket)
		pkt[0] = 0x47
		pkt[1] = byte((pid >> 8) & 0x1F)
		if i == 0 {
			pkt[1] |= 0x40
		}
		pkt[2] = byte(pid)
		pkt[3] = 0x30 | (cc & 0x0F)
		cc = (cc + 1) & 0x0F
		start := tsPacket - len(payload)
		pkt[4] = byte(start - 5)
		pkt[5] = 0x00
		for j := 6; j < start; j++ {
			pkt[j] = 0xFF
		}
		copy(pkt[start:], payload)
		out = append(out, pkt)
	}
	return out
}

func chunkOf(packets ...[]byte) []byte {
	var out []byte
	for _, p := range packets {
		out = append(out, p...)
	}
	return out
}

// largestPAT is 253 programmes, which is 1024 bytes - the most a section may be.
func largestPAT(t *testing.T, pmtPID uint16) []byte {
	t.Helper()
	programs := make([]program, 0, 253)
	programs = append(programs, program{1, pmtPID})
	for i := 1; i < 253; i++ {
		programs = append(programs, program{uint16(i + 100), 0x0900})
	}
	section := patSection(0, 0, 0, programs...)
	if len(section) != mediafacts.MaxSectionBytes {
		t.Fatalf("the fixture is %d bytes, not the largest a section may be", len(section))
	}
	return section
}

// bothCores runs one chunk through the reference and the real process and
// returns what each said, refusing anything that is not the coverage it should
// be.
func bothCores(t *testing.T, target uint16) (*mediafacts.GoCore, *RemoteCore, context.Context) {
	t.Helper()
	bin := requireRealCore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	remote, err := Start(ctx, bin, target)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := remote.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return mediafacts.NewGoCore(target), remote, ctx
}

func agree(t *testing.T, what string, goRes, rustRes mediafacts.ParseResult) {
	t.Helper()
	if !goRes.Covers(mediafacts.ParseCoverageComplete) {
		t.Fatalf("%s: the reference reported coverage %s", what, goRes.Coverage)
	}
	if !rustRes.Covers(mediafacts.ParseCoveragePSIOnly) {
		t.Fatalf("%s: the real core reported coverage %s", what, rustRes.Coverage)
	}
	if bad := scopeOf(goRes).diff(scopeOf(rustRes), "go  ", "rust"); len(bad) > 0 {
		t.Fatalf("%s:\n%s", what, joinLines(bad))
	}
}

func joinLines(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += "\n"
		}
		out += s
	}
	return out
}

// TestPSIProcess_FragmentationDoesNotChangeWhatCrossesTheBoundary is R8's
// central claim, asked of the real process.
//
// The same 1024-byte PAT arrives once in six packets and once in 1024, behind
// adaptation fields that leave one payload byte each. Both are legal deliveries
// of the same table. What comes back across the socket must be the same table,
// and the same table as the reference read - no carrier packet history crosses
// the boundary, because none is kept on either side of it.
func TestPSIProcess_FragmentationDoesNotChangeWhatCrossesTheBoundary(t *testing.T) {
	section := largestPAT(t, 256)

	normalPackets := psiPackets(0, 0, section)
	starved := starvedPackets(0, 0, section)
	if len(normalPackets) != 6 {
		t.Fatalf("the largest section took %d packets, expected 6", len(normalPackets))
	}
	if len(starved) != len(section) {
		t.Fatalf("the starved delivery is %d packets for a %d byte section", len(starved), len(section))
	}

	var normalResult, starvedResult mediafacts.ParseResult
	for _, tc := range []struct {
		name    string
		packets [][]byte
		into    *mediafacts.ParseResult
	}{
		{"normal", normalPackets, &normalResult},
		{"starved", starved, &starvedResult},
	} {
		local, remote, ctx := bothCores(t, 1)
		chunk := chunkOf(tc.packets...)

		goRes, err := local.Ingest(ctx, 0, chunk)
		if err != nil {
			t.Fatalf("%s: reference: %v", tc.name, err)
		}
		rustRes, err := remote.Ingest(ctx, 0, chunk)
		if err != nil {
			t.Fatalf("%s: real core: %v", tc.name, err)
		}
		agree(t, tc.name+" delivery", goRes, rustRes)
		*tc.into = rustRes
	}

	// And the two deliveries agree with each other, which is the invariance
	// itself rather than each one separately matching the reference.
	if normalResult.Facts.PMTPID != starvedResult.Facts.PMTPID {
		t.Errorf("PMT PID %d normal, %d starved", normalResult.Facts.PMTPID, starvedResult.Facts.PMTPID)
	}
	if len(normalResult.PSI.PATSections) != 1 || len(starvedResult.PSI.PATSections) != 1 {
		t.Fatalf("sections: %d normal, %d starved",
			len(normalResult.PSI.PATSections), len(starvedResult.PSI.PATSections))
	}
	if !bytes.Equal(normalResult.PSI.PATSections[0], starvedResult.PSI.PATSections[0]) {
		t.Errorf("the same PAT sent %d packets and %d packets crossed the boundary as two different tables",
			len(normalPackets), len(starved))
	}
	if !bytes.Equal(normalResult.PSI.PATSections[0], section) {
		t.Errorf("the section that came back is not the section that went in")
	}
}

// TestPSIProcess_ATableUsingEveryNumberItMayCrossesTheBoundary is the largest
// generation the syntax allows, over the socket.
//
// The Go reference could not walk this at all before R8 - a uint8 counter
// bounded by last_section_number 255 cannot end - and Rust never could not.
// Here it has to survive the frame, the bounds the decoder enforces, and the
// comparison.
func TestPSIProcess_ATableUsingEveryNumberItMayCrossesTheBoundary(t *testing.T) {
	local, remote, ctx := bothCores(t, 1)

	var packets [][]byte
	sections := make([][]byte, 0, mediafacts.MaxSectionsPerTable)
	cc := uint8(0)
	for n := 0; n < mediafacts.MaxSectionsPerTable; n++ {
		p := program{9, 0x0900}
		if n == mediafacts.MaxSectionsPerTable-1 {
			p = program{1, 256}
		}
		section := patSection(0, uint8(n), 255, p)
		sections = append(sections, section)
		packets = append(packets, psiPackets(0, cc, section)...)
		cc = (cc + 1) & 0x0F
	}
	chunk := chunkOf(packets...)

	goRes, err := local.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	rustRes, err := remote.Ingest(ctx, 0, chunk)
	if err != nil {
		t.Fatalf("real core: %v", err)
	}
	agree(t, "a 256-section PAT", goRes, rustRes)

	if rustRes.Facts.PMTPID != 256 {
		t.Errorf("the complete table did not name the target: PMT PID %d", rustRes.Facts.PMTPID)
	}
	if len(rustRes.PSI.PATSections) != mediafacts.MaxSectionsPerTable {
		t.Fatalf("%d sections crossed the boundary, want %d",
			len(rustRes.PSI.PATSections), mediafacts.MaxSectionsPerTable)
	}
	for i, got := range rustRes.PSI.PATSections {
		if !bytes.Equal(got, sections[i]) {
			t.Fatalf("section %d came back different", i)
		}
	}
	t.Logf("256 sections, %d bytes of table, crossed the boundary exactly", len(chunk))
}

// TestPSIProcess_ACarouselDoesNotGrowWhatCrossesTheBoundary measures the thing
// R8 was about, on the far side of the socket: repeating a table must not make
// the answer bigger.
func TestPSIProcess_ACarouselDoesNotGrowWhatCrossesTheBoundary(t *testing.T) {
	local, remote, ctx := bothCores(t, 1)
	section := largestPAT(t, 256)

	var first int
	offset := int64(0)
	for round := 0; round < 20; round++ {
		chunk := chunkOf(starvedPackets(0, uint8(round*3), section)...)
		goRes, err := local.Ingest(ctx, offset, chunk)
		if err != nil {
			t.Fatalf("round %d: reference: %v", round, err)
		}
		rustRes, err := remote.Ingest(ctx, offset, chunk)
		if err != nil {
			t.Fatalf("round %d: real core: %v", round, err)
		}
		offset += int64(len(chunk))
		agree(t, fmt.Sprintf("carousel round %d", round), goRes, rustRes)

		held := 0
		for _, s := range rustRes.PSI.PATSections {
			held += len(s)
		}
		for _, s := range rustRes.PSI.PMTSections {
			held += len(s)
		}
		if round == 0 {
			first = held
			continue
		}
		if held != first {
			t.Fatalf("round %d carries %d bytes of table, round 1 carried %d", round, held, first)
		}
	}
	t.Logf("20 starved carousel rounds: %d bytes of table each time", first)
}

// TestPSIProcess_TheRoundTripCostOfAlignedChunks is the measurement, at sizes
// the ring actually produces.
//
// Every size is whole packets, and the byte counts are reported rather than the
// round numbers they are near: nobody should later read "4 MiB" and conclude
// 4,194,304 was sent, because it was not - that is not a whole number of
// transport packets.
func TestPSIProcess_TheRoundTripCostOfAlignedChunks(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement, not a smoke test")
	}
	local, remote, ctx := bothCores(t, 1)

	// A representative transport: PSI at the front, then payload the parser
	// walks past. The PAT is what makes the parse non-trivial.
	section := patSection(0, 0, 0, program{1, 256})
	head := psiPackets(0, 0, section)

	for _, target := range []int{64 * 1024, 256 * 1024, 1024 * 1024, 4 * 1024 * 1024} {
		packets := target / tsPacket
		chunk := make([]byte, 0, packets*tsPacket)
		chunk = append(chunk, chunkOf(head...)...)
		filler := make([]byte, tsPacket)
		filler[0] = 0x47
		filler[1] = 0x1F
		filler[2] = 0xFF
		filler[3] = 0x10
		for len(chunk) < packets*tsPacket {
			chunk = append(chunk, filler...)
		}
		if len(chunk)%tsPacket != 0 {
			t.Fatalf("the fixture is %d bytes, which is not whole packets", len(chunk))
		}

		// Both cores are given the same calls in the same order, and only the
		// remote one is timed. Feeding the reference once and the remote fifty
		// times would compare a first result against a fiftieth: a repeated
		// table is not a change, so the reference would still be reporting the
		// identity event the remote stopped reporting forty-nine calls ago.
		const rounds = 50
		took := make([]time.Duration, 0, rounds)
		var goRes, rustRes mediafacts.ParseResult
		var err error
		for i := 0; i < rounds; i++ {
			if goRes, err = local.Ingest(ctx, 0, chunk); err != nil {
				t.Fatalf("%d bytes, round %d: reference: %v", len(chunk), i, err)
			}
			began := time.Now()
			rustRes, err = remote.Ingest(ctx, 0, chunk)
			if err != nil {
				t.Fatalf("%d bytes, round %d: %v", len(chunk), i, err)
			}
			took = append(took, time.Since(began))
		}
		agree(t, fmt.Sprintf("%d bytes", len(chunk)), goRes, rustRes)

		sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
		p := func(q float64) time.Duration { return took[int(float64(len(took)-1)*q)] }
		t.Logf("%9d bytes (%5d packets)  p50=%-12v p99=%-12v max=%v",
			len(chunk), len(chunk)/tsPacket, p(0.50), p(0.99), took[len(took)-1])
	}
	t.Logf("deadline for one call: %v", mediafacts.DefaultIngestDeadline)
}
