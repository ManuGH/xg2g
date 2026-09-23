// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

var updateTimingCorpus = flag.Bool("update-timing-corpus", false, "regenerate testdata/timing-corpus/corpus.txt")

const timingCorpusRelativePath = "../../../../../testdata/timing-corpus/corpus.txt"

// --- MPEG-TS Packet Construction Helpers ------------------------------------

func tsCRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		for bit := 7; bit >= 0; bit-- {
			top := (crc >> 31) & 1
			in := uint32((b >> uint(bit)) & 1)
			crc <<= 1
			if top^in == 1 {
				crc ^= 0x04C11DB7
			}
		}
	}
	return crc
}

func tsSeal(body []byte) []byte {
	crc := tsCRC32(body)
	return append(body, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

func tsSection(tableID byte, idExt uint16, version byte, payload []byte) []byte {
	sectionLen := 9 + len(payload)
	body := []byte{
		tableID,
		0xB0 | byte((sectionLen>>8)&0x0F),
		byte(sectionLen & 0xFF),
		byte(idExt >> 8),
		byte(idExt & 0xFF),
		0xC1 | ((version & 0x1F) << 1),
		0x00,
		0x00,
	}
	body = append(body, payload...)
	return tsSeal(body)
}

func makePATPacket(program, pmtPID uint16, cc byte) []byte {
	payload := []byte{
		byte(program >> 8), byte(program & 0xFF),
		0xE0 | byte(pmtPID>>8), byte(pmtPID & 0xFF),
	}
	sec := tsSection(0x00, 1, 0, payload)
	p := make([]byte, 188)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = 0x47
	p[1] = 0x40 // PUSI=1, PID=0
	p[2] = 0x00
	p[3] = 0x10 | (cc & 0x0F)
	p[4] = 0x00 // pointer field
	copy(p[5:], sec)
	return p
}

type esDesc struct {
	streamType byte
	pid        uint16
}

func makePMTPacket(program uint16, version byte, pcrPID uint16, cc byte, streams ...esDesc) []byte {
	payload := []byte{
		0xE0 | byte(pcrPID>>8), byte(pcrPID & 0xFF),
		0xF0, 0x00, // program info length = 0
	}
	for _, s := range streams {
		payload = append(payload,
			s.streamType,
			0xE0|byte(s.pid>>8), byte(s.pid&0xFF),
			0xF0, 0x00, // ES info length = 0
		)
	}
	sec := tsSection(0x02, program, version, payload)
	p := make([]byte, 188)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = 0x47
	p[1] = 0x40 | byte((pcrPID>>8)&0x1F) // PUSI=1
	p[2] = byte(pcrPID & 0xFF)
	p[3] = 0x10 | (cc & 0x0F)
	p[4] = 0x00 // pointer field
	copy(p[5:], sec)
	return p
}

func makePCRPacket(pid uint16, cc byte, di bool, pcr27m *uint64) []byte {
	p := make([]byte, 188)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = 0x47
	p[1] = byte((pid >> 8) & 0x1F)
	p[2] = byte(pid & 0xFF)
	p[3] = 0x20 | (cc & 0x0F) // adaptation only
	p[4] = 183                // adaptation field length

	var flags byte
	if di {
		flags |= 0x80
	}
	if pcr27m != nil {
		flags |= 0x10
	}
	p[5] = flags

	if pcr27m != nil {
		val := *pcr27m
		base := val / 300
		ext := val % 300
		p[6] = byte(base >> 25)
		p[7] = byte((base >> 17) & 0xFF)
		p[8] = byte((base >> 9) & 0xFF)
		p[9] = byte((base >> 1) & 0xFF)
		p[10] = byte(((base & 1) << 7) | 0x7E | ((ext >> 8) & 1))
		p[11] = byte(ext & 0xFF)
	}
	return p
}

func encodeTimestamp(ts uint64, prefix byte) [5]byte {
	return [5]byte{
		(prefix << 4) | byte(((ts>>29)&0x0E)|1),
		byte(ts >> 22),
		byte(((ts >> 14) & 0xFE) | 1),
		byte(ts >> 7),
		byte(((ts << 1) & 0xFE) | 1),
	}
}

func makeVideoPESPacket(pid uint16, cc byte, pts *uint64, dts *uint64, isIDR bool) []byte {
	var nalPayload []byte
	if isIDR {
		// SPS, PPS, IDR slice
		nalPayload = []byte{
			0x00, 0x00, 0x01, 0x67, 0x42, 0xC0, 0x1E, 0xDA, 0x02, 0x80, 0xF6, 0x80,
			0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80,
			0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x21, 0xA0, 0x33, 0xFF,
		}
	} else {
		// Non-IDR slice
		nalPayload = []byte{0x00, 0x00, 0x01, 0x41, 0xC0, 0x21, 0xA0, 0x33, 0xFF}
	}
	return makeVideoPESPacketES(pid, cc, pts, dts, nalPayload)
}

// makeVideoPESPacketES is a PES start on pid carrying pts/dts and then es.
func makeVideoPESPacketES(pid uint16, cc byte, pts *uint64, dts *uint64, es []byte) []byte {
	p := make([]byte, 188)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = 0x47
	p[1] = 0x40 | byte((pid>>8)&0x1F) // PUSI = 1
	p[2] = byte(pid & 0xFF)
	p[3] = 0x10 | (cc & 0x0F)

	pesHdr := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80}
	var flags2 byte
	var optHdr []byte

	if pts != nil && dts != nil {
		flags2 = 0xC0 // PTS + DTS
		optHdr = make([]byte, 1+10)
		optHdr[0] = 10
		pBytes := encodeTimestamp(*pts, 0b0011)
		copy(optHdr[1:6], pBytes[:])
		dBytes := encodeTimestamp(*dts, 0b0001)
		copy(optHdr[6:11], dBytes[:])
	} else if pts != nil {
		flags2 = 0x80 // PTS only
		optHdr = make([]byte, 1+5)
		optHdr[0] = 5
		pBytes := encodeTimestamp(*pts, 0b0010)
		copy(optHdr[1:6], pBytes[:])
	} else {
		flags2 = 0x00
		optHdr = []byte{0x00}
	}
	pesHdr = append(pesHdr, flags2)
	pesHdr = append(pesHdr, optHdr...)

	payload := append(pesHdr, es...)
	copy(p[4:], payload)
	return p
}

func makeVideoContPacket(pid uint16, cc byte, tei bool) []byte {
	p := make([]byte, 188)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = 0x47
	p[1] = byte((pid >> 8) & 0x1F)
	if tei {
		p[1] |= 0x80
	}
	p[2] = byte(pid & 0xFF)
	p[3] = 0x10 | (cc & 0x0F)
	// Some non-start slice data
	copy(p[4:], []byte{0xAA, 0xBB, 0xCC, 0xDD})
	return p
}

func makeAudioPacket(pid uint16, cc byte) []byte {
	p := make([]byte, 188)
	for i := range p {
		p[i] = 0xFF
	}
	p[0] = 0x47
	p[1] = byte((pid >> 8) & 0x1F)
	p[2] = byte(pid & 0xFF)
	p[3] = 0x10 | (cc & 0x0F)
	// Audio payload
	copy(p[4:], []byte{0x0B, 0x77, 0x01, 0x02, 0x03, 0x04})
	return p
}

// --- Corpus Data Structures -------------------------------------------------

type expectedTimingRecord struct {
	kind        string // "pes", "pcr", "disc"
	epoch       uint64
	hasEpoch    bool
	pid         uint16
	pts         int64
	hasPTS      bool
	dts         int64
	hasDTS      bool
	obs         int64
	sub         int64
	pcr         int64
	scope       string
	reason      string
	epochBefore int64
	hasBefore   bool
	epochAfter  int64
	hasAfter    bool
}

type expectedEvent struct {
	kind     string // "identity", "rap", "rap_invalidated"
	offset   int64
	joinable bool
}

type timingStep struct {
	desc     string
	chunk    []byte
	hasEpoch bool
	epoch    uint64
	records  []expectedTimingRecord
	events   []expectedEvent
}

type timingCorpusCase struct {
	name    string
	desc    string
	program uint16
	steps   []timingStep
}

func buildTimingCorpusCases() []timingCorpusCase {
	var cases []timingCorpusCase

	// Case 1: The Canonical Timeline Lifecycle (covering 13 sequential steps)
	var steps []timingStep

	// Step 0: PAT before PMT
	steps = append(steps, timingStep{
		desc:     "PAT arrives before any PMT: program identity changes, epoch is deactivated",
		chunk:    makePATPacket(1, 256, 0),
		hasEpoch: false,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:      "disc",
			scope:     "program",
			reason:    "program_identity_changed",
			obs:       0,
			hasBefore: false,
			hasAfter:  false,
		}},
	})

	// Step 1: First PMT Epoch 0
	pmtStreams := []esDesc{
		{streamType: 0x1B, pid: 257}, // H.264
		{streamType: 0x03, pid: 258}, // MP2 Audio
	}
	steps = append(steps, timingStep{
		desc:     "First PMT accepted: establishes Epoch 0 and emits program identity discontinuity",
		chunk:    makePMTPacket(1, 0, 256, 0, pmtStreams...),
		hasEpoch: true,
		epoch:    0,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:       "disc",
			scope:      "program",
			reason:     "program_identity_changed",
			obs:        188,
			hasBefore:  false,
			hasAfter:   true,
			epochAfter: 0,
		}},
	})

	// Step 2: PCR-first alignment
	pcrVal1 := uint64(27_000_000) // 1s at 27MHz = 90000 ticks at 90kHz
	steps = append(steps, timingStep{
		desc:     "PCR-first arrives: sets initial epoch phase reference",
		chunk:    makePCRPacket(256, 0, false, &pcrVal1),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pcr",
			epoch:    0,
			hasEpoch: true,
			pid:      256,
			pcr:      27_000_000,
			obs:      376,
		}},
	})

	// Step 3: PTS + DTS & RAP timing binding
	ptsVal1 := uint64(90_000)
	dtsVal1 := uint64(86_400)
	steps = append(steps, timingStep{
		desc:     "Video PES with PTS+DTS and IDR slice: emits RAP bound to PTS",
		chunk:    makeVideoPESPacket(257, 0, &ptsVal1, &dtsVal1, true),
		hasEpoch: true,
		epoch:    0,
		events:   []expectedEvent{{kind: "rap", offset: 564, joinable: true}},
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      90_000,
			hasDTS:   true,
			dts:      86_400,
			obs:      564,
			sub:      564,
		}, {
			// The IDR establishes the RAP in its own PES start packet.
			kind:     "rap",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      90_000,
			hasDTS:   true,
			dts:      86_400,
			obs:      564,
			sub:      564,
		}},
	})

	// Step 4: PTS only
	ptsVal2 := uint64(93_600)
	steps = append(steps, timingStep{
		desc:     "Video PES with PTS only and non-IDR slice",
		chunk:    makeVideoPESPacket(257, 1, &ptsVal2, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      93_600,
			hasDTS:   false,
			obs:      752,
			sub:      752,
		}},
	})

	// Step 5: negative/B-frame PTS
	ptsVal3 := uint64(86_400) // negative delta from 93600 anchor (-7200)
	steps = append(steps, timingStep{
		desc:     "Video PES with B-frame negative delta relative to anchor",
		chunk:    makeVideoPESPacket(257, 2, &ptsVal3, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      86_400,
			hasDTS:   false,
			obs:      940,
			sub:      940,
		}},
	})

	// Step 6: RAP invalidation
	// Packet A: PUSI with IDR at 1128 -> emits provisional RAP
	// Packet B: Corrupted packet with TEI at 1316 -> invalidates RAP and resets track
	p1 := makeVideoPESPacket(257, 3, &ptsVal1, nil, true)
	p2 := makeVideoContPacket(257, 4, true)
	steps = append(steps, timingStep{
		desc:     "Provisional RAP followed by TEI in same AU invalidates RAP and reports track reset",
		chunk:    append(p1, p2...),
		hasEpoch: true,
		epoch:    0,
		events: []expectedEvent{
			{kind: "rap", offset: 1128, joinable: true},
			{kind: "rap_invalidated", offset: 1128},
		},
		records: []expectedTimingRecord{
			{
				kind:     "pes",
				epoch:    0,
				hasEpoch: true,
				pid:      257,
				hasPTS:   true,
				pts:      90_000,
				hasDTS:   false,
				obs:      1128,
				sub:      1128,
			},
			{
				// Published with the provisional RAP; the invalidation event retracts both.
				kind:     "rap",
				epoch:    0,
				hasEpoch: true,
				pid:      257,
				hasPTS:   true,
				pts:      90_000,
				hasDTS:   false,
				obs:      1128,
				sub:      1128,
			},
			{
				kind:        "disc",
				scope:       "track:257",
				reason:      "transport_timing_loss",
				obs:         1316,
				hasBefore:   true,
				epochBefore: 0,
				hasAfter:    true,
				epochAfter:  0,
			},
		},
	})

	// Step 7: DI without PCR advances epoch once; next clean PCR stays in that epoch
	pcrVal2 := uint64(54_000_000)
	steps = append(steps, timingStep{
		desc:     "DI without PCR on PCR PID advances epoch from 0 to 1",
		chunk:    makePCRPacket(256, 1, true, nil),
		hasEpoch: true,
		epoch:    1,
		records: []expectedTimingRecord{{
			kind:        "disc",
			scope:       "program",
			reason:      "pcr_discontinuity_indicator",
			obs:         1504,
			hasBefore:   true,
			epochBefore: 0,
			hasAfter:    true,
			epochAfter:  1,
		}},
	})
	steps = append(steps, timingStep{
		desc:     "Next clean PCR sample remains in epoch 1 with no second discontinuity",
		chunk:    makePCRPacket(256, 2, false, &pcrVal2),
		hasEpoch: true,
		epoch:    1,
		records: []expectedTimingRecord{{
			kind:     "pcr",
			epoch:    1,
			hasEpoch: true,
			pid:      256,
			pcr:      54_000_000,
			obs:      1692,
		}},
	})

	// Step 8: DI + PCR in same packet
	pcrVal3 := uint64(81_000_000)
	steps = append(steps, timingStep{
		desc:     "DI and PCR in same packet advances epoch from 1 to 2 and records sample directly in epoch 2",
		chunk:    makePCRPacket(256, 3, true, &pcrVal3),
		hasEpoch: true,
		epoch:    2,
		records: []expectedTimingRecord{
			{
				kind:        "disc",
				scope:       "program",
				reason:      "pcr_discontinuity_indicator",
				obs:         1880,
				hasBefore:   true,
				epochBefore: 1,
				hasAfter:    true,
				epochAfter:  2,
			},
			{
				kind:     "pcr",
				epoch:    2,
				hasEpoch: true,
				pid:      256,
				pcr:      81_000_000,
				obs:      1880,
			},
		},
	})

	// Step 9: DTS-first alignment in new epoch
	ptsVal4 := uint64(270_000)
	dtsVal4 := uint64(266_400)
	steps = append(steps, timingStep{
		desc:     "In new epoch, video PES with DTS arrives before PCR and establishes phase reference",
		chunk:    makeVideoPESPacket(257, 5, &ptsVal4, &dtsVal4, false),
		hasEpoch: true,
		epoch:    2,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    2,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      270_000,
			hasDTS:   true,
			dts:      266_400,
			obs:      2068,
			sub:      2068,
		}},
	})

	// Step 10: Video track-local reset
	// Video packet with CC jump (skips from 5 to 9 without DI)
	steps = append(steps, timingStep{
		desc:     "Video packet CC jump triggers track-local reset without advancing epoch",
		chunk:    makeVideoPESPacket(257, 9, &ptsVal4, nil, false),
		hasEpoch: true,
		epoch:    2,
		records: []expectedTimingRecord{
			{
				kind:        "disc",
				scope:       "track:257",
				reason:      "transport_timing_loss",
				obs:         2256,
				hasBefore:   true,
				epochBefore: 2,
				hasAfter:    true,
				epochAfter:  2,
			},
			{
				kind:     "pes",
				epoch:    2,
				hasEpoch: true,
				pid:      257,
				hasPTS:   true,
				pts:      270_000,
				hasDTS:   false,
				obs:      2256,
				sub:      2256,
			},
		},
	})

	// Step 11: PMT replacement advances epoch from 2 to 3
	steps = append(steps, timingStep{
		desc:     "PMT version increment advances epoch from 2 to 3",
		chunk:    makePMTPacket(1, 1, 256, 1, pmtStreams...),
		hasEpoch: true,
		epoch:    3,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:        "disc",
			scope:       "program",
			reason:      "program_identity_changed",
			obs:         2444,
			hasBefore:   true,
			epochBefore: 2,
			hasAfter:    true,
			epochAfter:  3,
		}},
	})

	// Step 12: Audio track-local reset
	// Audio packet with CC jump
	pAudio1 := makeAudioPacket(258, 0)
	pAudio2 := makeAudioPacket(258, 5) // CC skip 0 -> 5
	steps = append(steps, timingStep{
		desc:     "Audio packet CC jump triggers audio track-local reset without advancing epoch",
		chunk:    append(pAudio1, pAudio2...),
		hasEpoch: true,
		epoch:    3,
		records: []expectedTimingRecord{{
			kind:        "disc",
			scope:       "track:258",
			reason:      "transport_timing_loss",
			obs:         2820,
			hasBefore:   true,
			epochBefore: 3,
			hasAfter:    true,
			epochAfter:  3,
		}},
	})

	cases = append(cases, timingCorpusCase{
		name:    "canonical_timeline_lifecycle",
		desc:    "authoritative multi-step stream verifying canonical timing publication across protocol v6",
		program: 1,
		steps:   steps,
	})

	// Case 2: PTS Wrap across 33-bit boundary (2^33 ticks)
	var ptsWrapSteps []timingStep
	ptsWrapSteps = append(ptsWrapSteps, timingStep{
		desc:     "PAT for program 1",
		chunk:    makePATPacket(1, 256, 0),
		hasEpoch: false,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:      "disc",
			scope:     "program",
			reason:    "program_identity_changed",
			obs:       0,
			hasBefore: false,
			hasAfter:  false,
		}},
	})
	ptsWrapSteps = append(ptsWrapSteps, timingStep{
		desc:     "PMT for program 1",
		chunk:    makePMTPacket(1, 0, 256, 0, pmtStreams...),
		hasEpoch: true,
		epoch:    0,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:       "disc",
			scope:      "program",
			reason:     "program_identity_changed",
			obs:        188,
			hasBefore:  false,
			hasAfter:   true,
			epochAfter: 0,
		}},
	})
	ptsBeforeWrap := uint64((1 << 33) - 500)
	ptsWrapSteps = append(ptsWrapSteps, timingStep{
		desc:     "Video PES near 33-bit boundary (2^33 - 500)",
		chunk:    makeVideoPESPacket(257, 0, &ptsBeforeWrap, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      int64(ptsBeforeWrap),
			hasDTS:   false,
			obs:      376,
			sub:      376,
		}},
	})
	ptsAfterWrapRaw := uint64(500)
	ptsWrapSteps = append(ptsWrapSteps, timingStep{
		desc:     "Video PES wraps across 2^33 boundary to raw 500: correctly unrolls to 2^33 + 500",
		chunk:    makeVideoPESPacket(257, 1, &ptsAfterWrapRaw, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      int64((1 << 33) + 500),
			hasDTS:   false,
			obs:      564,
			sub:      564,
		}},
	})
	cases = append(cases, timingCorpusCase{
		name:    "pts_wrap_33bit",
		desc:    "verifies seamless 33-bit PTS unrolling across modulus boundary",
		program: 1,
		steps:   ptsWrapSteps,
	})

	// Case 3: PCR Wrap across 27 MHz boundary (2^33 * 300)
	var pcrWrapSteps []timingStep
	pcrWrapSteps = append(pcrWrapSteps, timingStep{
		desc:     "PAT for program 1",
		chunk:    makePATPacket(1, 256, 0),
		hasEpoch: false,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:      "disc",
			scope:     "program",
			reason:    "program_identity_changed",
			obs:       0,
			hasBefore: false,
			hasAfter:  false,
		}},
	})
	pcrWrapSteps = append(pcrWrapSteps, timingStep{
		desc:     "PMT for program 1",
		chunk:    makePMTPacket(1, 0, 256, 0, pmtStreams...),
		hasEpoch: true,
		epoch:    0,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:       "disc",
			scope:      "program",
			reason:     "program_identity_changed",
			obs:        188,
			hasBefore:  false,
			hasAfter:   true,
			epochAfter: 0,
		}},
	})
	pcrModulus := uint64((1 << 33) * 300)
	pcrBeforeWrap := pcrModulus - 3000
	pcrWrapSteps = append(pcrWrapSteps, timingStep{
		desc:     "PCR near 27 MHz boundary",
		chunk:    makePCRPacket(256, 0, false, &pcrBeforeWrap),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pcr",
			epoch:    0,
			hasEpoch: true,
			pid:      256,
			pcr:      int64(pcrBeforeWrap),
			obs:      376,
		}},
	})
	pcrAfterWrapRaw := uint64(3000)
	pcrWrapSteps = append(pcrWrapSteps, timingStep{
		desc:     "PCR wraps across 27 MHz boundary to raw 3000: correctly unrolls to modulus + 3000",
		chunk:    makePCRPacket(256, 1, false, &pcrAfterWrapRaw),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pcr",
			epoch:    0,
			hasEpoch: true,
			pid:      256,
			pcr:      int64(pcrModulus + 3000),
			obs:      564,
		}},
	})
	cases = append(cases, timingCorpusCase{
		name:    "pcr_wrap_27mhz",
		desc:    "verifies seamless 27 MHz PCR unrolling across modulus boundary",
		program: 1,
		steps:   pcrWrapSteps,
	})

	// Case 4: Track Re-Anchor beyond half modulus (> 13.25h)
	var reAnchorSteps []timingStep
	reAnchorSteps = append(reAnchorSteps, timingStep{
		desc:     "PAT for program 1",
		chunk:    makePATPacket(1, 256, 0),
		hasEpoch: false,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:      "disc",
			scope:     "program",
			reason:    "program_identity_changed",
			obs:       0,
			hasBefore: false,
			hasAfter:  false,
		}},
	})
	reAnchorSteps = append(reAnchorSteps, timingStep{
		desc:     "PMT for program 1",
		chunk:    makePMTPacket(1, 0, 256, 0, pmtStreams...),
		hasEpoch: true,
		epoch:    0,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind:       "disc",
			scope:      "program",
			reason:     "program_identity_changed",
			obs:        188,
			hasBefore:  false,
			hasAfter:   true,
			epochAfter: 0,
		}},
	})
	pts0 := uint64(0)
	reAnchorSteps = append(reAnchorSteps, timingStep{
		desc:     "Stream origin at PTS 0",
		chunk:    makeVideoPESPacket(257, 0, &pts0, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      0,
			hasDTS:   false,
			obs:      376,
			sub:      376,
		}},
	})
	ptsProg1 := uint64(3_000_000_000)
	reAnchorSteps = append(reAnchorSteps, timingStep{
		desc:     "Stream progresses to 3,000,000,000 ticks (~9.25h)",
		chunk:    makeVideoPESPacket(257, 1, &ptsProg1, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      3_000_000_000,
			hasDTS:   false,
			obs:      564,
			sub:      564,
		}},
	})
	ptsProg2 := uint64(5_000_000_000) // > M/2 (4,294,967,296)
	reAnchorSteps = append(reAnchorSteps, timingStep{
		desc:     "Stream progresses beyond M/2 to 5,000,000,000 ticks (~15.4h)",
		chunk:    makeVideoPESPacket(257, 2, &ptsProg2, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind:     "pes",
			epoch:    0,
			hasEpoch: true,
			pid:      257,
			hasPTS:   true,
			pts:      5_000_000_000,
			hasDTS:   false,
			obs:      752,
			sub:      752,
		}},
	})
	// CC jump (2 -> 6) resets track
	ptsProg3 := uint64(5_000_003_600)
	reAnchorSteps = append(reAnchorSteps, timingStep{
		desc:     "CC jump resets track: subsequent sample re-anchors against dynamic phase reference, NOT origin 0",
		chunk:    makeVideoPESPacket(257, 6, &ptsProg3, nil, false),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{
			{
				kind:        "disc",
				scope:       "track:257",
				reason:      "transport_timing_loss",
				obs:         940,
				hasBefore:   true,
				epochBefore: 0,
				hasAfter:    true,
				epochAfter:  0,
			},
			{
				kind:     "pes",
				epoch:    0,
				hasEpoch: true,
				pid:      257,
				hasPTS:   true,
				pts:      5_000_003_600, // Re-anchored to ~15.4h, NOT jumping back to ~0
				hasDTS:   false,
				obs:      940,
				sub:      940,
			},
		},
	})
	cases = append(cases, timingCorpusCase{
		name:    "re_anchor_beyond_half_modulus",
		desc:    "verifies track re-anchoring beyond 13.25h aligns against phase reference without jumping backward",
		program: 1,
		steps:   reAnchorSteps,
	})

	// Case: a RAP established only when its access unit ends. An all-intra H.264
	// picture (non-IDR slices whose headers all say I) is not an entry point until
	// the next PES shows that no predicted slice belongs to it - so the RAP event
	// arrives two chunks after the PES record that carries its timing. The binding
	// must arrive with the event, not stay behind with the header.
	var delayedSteps []timingStep
	delayedSteps = append(delayedSteps, timingStep{
		desc:     "PAT arrives before any PMT",
		chunk:    makePATPacket(1, 256, 0),
		hasEpoch: false,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind: "disc", scope: "program", reason: "program_identity_changed", obs: 0,
		}},
	})
	delayedSteps = append(delayedSteps, timingStep{
		desc:     "PMT establishes Epoch 0",
		chunk:    makePMTPacket(1, 0, 256, 0, esDesc{streamType: 0x1B, pid: 257}),
		hasEpoch: true,
		epoch:    0,
		events:   []expectedEvent{{kind: "identity"}},
		records: []expectedTimingRecord{{
			kind: "disc", scope: "program", reason: "program_identity_changed", obs: 188,
			hasAfter: true, epochAfter: 0,
		}},
	})
	allIntraPTS, allIntraDTS := uint64(180_000), uint64(176_400)
	delayedSteps = append(delayedSteps, timingStep{
		desc: "All-intra access unit starts: SPS, PPS, non-IDR slice with slice_type I - no RAP yet",
		chunk: makeVideoPESPacketES(257, 0, &allIntraPTS, &allIntraDTS, []byte{
			0x00, 0x00, 0x01, 0x67, 0x42, 0xC0, 0x1E, 0xDA, 0x02, 0x80, 0xF6, 0x80, // SPS
			0x00, 0x00, 0x01, 0x68, 0xCE, 0x38, 0x80, // PPS
			0x00, 0x00, 0x01, 0x41, 0xB0, 0x21, 0xA0, 0x33, 0xFF, // non-IDR slice, slice_type I
		}),
		hasEpoch: true,
		epoch:    0,
		records: []expectedTimingRecord{{
			kind: "pes", epoch: 0, hasEpoch: true, pid: 257,
			hasPTS: true, pts: 180_000, hasDTS: true, dts: 176_400, obs: 376, sub: 376,
		}},
	})
	delayedSteps = append(delayedSteps, timingStep{
		desc:     "The access unit continues in its own chunk",
		chunk:    makeVideoContPacket(257, 1, false),
		hasEpoch: true,
		epoch:    0,
	})
	nextPTS := uint64(183_600)
	delayedSteps = append(delayedSteps, timingStep{
		desc:     "Next PES ends the all-intra access unit: RAP at 376 is established here and published bound to its own PES timing",
		chunk:    makeVideoPESPacket(257, 2, &nextPTS, nil, false),
		hasEpoch: true,
		epoch:    0,
		events:   []expectedEvent{{kind: "rap", offset: 376, joinable: true}},
		records: []expectedTimingRecord{
			{
				kind: "pes", epoch: 0, hasEpoch: true, pid: 257,
				hasPTS: true, pts: 183_600, obs: 752, sub: 752,
			},
			{
				kind: "rap", epoch: 0, hasEpoch: true, pid: 257,
				hasPTS: true, pts: 180_000, hasDTS: true, dts: 176_400, obs: 752, sub: 376,
			},
		},
	})
	cases = append(cases, timingCorpusCase{
		name:    "rap_established_at_access_unit_end",
		desc:    "an all-intra RAP established chunks after its PES header is published bound in the chunk that establishes it",
		program: 1,
		steps:   delayedSteps,
	})

	return cases
}

func renderTimingCorpus(cases []timingCorpusCase) string {
	var sb strings.Builder
	sb.WriteString("# xg2g canonical timing publication corpus, format version 1\n")
	sb.WriteString("#\n")
	sb.WriteString("# A case defines an MPEG-TS stream and the exact canonical timeline\n")
	sb.WriteString("# publication that Rust media-core must emit and Go remotecore must decode.\n")
	sb.WriteString("version 1\n\n")

	for _, c := range cases {
		sb.WriteString(fmt.Sprintf("case %s\n", c.name))
		sb.WriteString(fmt.Sprintf("  desc %s\n", c.desc))
		sb.WriteString(fmt.Sprintf("  program %d\n", c.program))

		for _, s := range c.steps {
			sb.WriteString(fmt.Sprintf("  # %s\n", s.desc))
			sb.WriteString(fmt.Sprintf("  chunk %s\n", hex.EncodeToString(s.chunk)))
			if s.hasEpoch {
				sb.WriteString(fmt.Sprintf("  epoch %d\n", s.epoch))
			} else {
				sb.WriteString("  epoch none\n")
			}
			for _, ev := range s.events {
				switch ev.kind {
				case "identity":
					sb.WriteString("  event identity\n")
				case "rap":
					join := 0
					if ev.joinable {
						join = 1
					}
					sb.WriteString(fmt.Sprintf("  event rap offset=%d joinable=%d\n", ev.offset, join))
				case "rap_invalidated":
					sb.WriteString(fmt.Sprintf("  event rap_invalidated offset=%d\n", ev.offset))
				}
			}
			for _, rec := range s.records {
				switch rec.kind {
				case "pes", "rap":
					ptsStr := "none"
					if rec.hasPTS {
						ptsStr = strconv.FormatInt(rec.pts, 10)
					}
					dtsStr := "none"
					if rec.hasDTS {
						dtsStr = strconv.FormatInt(rec.dts, 10)
					}
					sb.WriteString(fmt.Sprintf("  record %s epoch=%d pid=%d pts=%s dts=%s obs=%d sub=%d\n",
						rec.kind, rec.epoch, rec.pid, ptsStr, dtsStr, rec.obs, rec.sub))
				case "pcr":
					sb.WriteString(fmt.Sprintf("  record pcr epoch=%d pid=%d pcr=%d obs=%d\n",
						rec.epoch, rec.pid, rec.pcr, rec.obs))
				case "disc":
					beforeStr := "none"
					if rec.hasBefore {
						beforeStr = strconv.FormatInt(rec.epochBefore, 10)
					}
					afterStr := "none"
					if rec.hasAfter {
						afterStr = strconv.FormatInt(rec.epochAfter, 10)
					}
					sb.WriteString(fmt.Sprintf("  record disc scope=%s reason=%s obs=%d before=%s after=%s\n",
						rec.scope, rec.reason, rec.obs, beforeStr, afterStr))
				}
			}
		}
		sb.WriteString("end\n\n")
	}

	return sb.String()
}

// TestGenerateTimingCorpus writes the corpus if -update-timing-corpus is passed.
func TestGenerateTimingCorpus(t *testing.T) {
	if !*updateTimingCorpus {
		t.Skip("skipping corpus generation without -update-timing-corpus")
	}
	cases := buildTimingCorpusCases()
	content := renderTimingCorpus(cases)
	path, err := filepath.Abs(timingCorpusRelativePath)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write timing corpus: %v", err)
	}
	t.Logf("successfully wrote %s (%d bytes)", path, len(content))
}

// TestTimingCorpus_LiveMediaCorePublication tests that the live Rust media-core
// publishes the exact authored timeline over protocol v7 and Go RemoteCore decodes it.
func TestTimingCorpus_LiveMediaCorePublication(t *testing.T) {
	coreBin := requireVideoRealCore(t)
	cases := buildTimingCorpusCases()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			core, err := Start(ctx, coreBin, c.program)
			if err != nil {
				t.Fatalf("start remote core: %v", err)
			}
			defer func() { _ = core.Close() }()

			var currentOffset int64

			for stepIdx, s := range c.steps {
				t.Logf("Step %d: %s", stepIdx, s.desc)
				res, err := core.Ingest(ctx, currentOffset, s.chunk)
				if err != nil {
					t.Fatalf("step %d ingest error: %v", stepIdx, err)
				}
				currentOffset = res.ProcessedThroughOffset

				// 1. Verify Timing Authority
				if res.Timing.Authority != mediafacts.TimingAuthorityCanonical {
					t.Errorf("step %d: authority %s, want canonical", stepIdx, res.Timing.Authority)
				}

				// 2. Verify Active Epoch
				if s.hasEpoch != res.Timing.HasActiveEpoch {
					t.Errorf("step %d: has_epoch=%v, want %v", stepIdx, res.Timing.HasActiveEpoch, s.hasEpoch)
				}
				if s.hasEpoch && res.Timing.ActiveEpoch != mediafacts.TimelineEpoch(s.epoch) {
					t.Errorf("step %d: epoch=%d, want %d", stepIdx, res.Timing.ActiveEpoch, s.epoch)
				}

				// 3. Verify Events
				if len(res.Events) != len(s.events) {
					t.Errorf("step %d: got %d events, want %d: %+v", stepIdx, len(res.Events), len(s.events), res.Events)
				} else {
					for i, ev := range s.events {
						gotEv := res.Events[i]
						switch ev.kind {
						case "identity":
							if gotEv.Kind != mediafacts.EventProgramIdentityChanged {
								t.Errorf("step %d event %d: got %v, want identity", stepIdx, i, gotEv.Kind)
							}
						case "rap":
							if gotEv.Kind != mediafacts.EventRandomAccessPoint || gotEv.Offset != ev.offset || gotEv.Joinable != ev.joinable {
								t.Errorf("step %d event %d: got %+v, want rap offset=%d joinable=%v", stepIdx, i, gotEv, ev.offset, ev.joinable)
							}
						case "rap_invalidated":
							if gotEv.Kind != mediafacts.EventRandomAccessPointInvalidated || gotEv.Offset != ev.offset {
								t.Errorf("step %d event %d: got %+v, want rap_invalidated offset=%d", stepIdx, i, gotEv, ev.offset)
							}
						}
					}
				}

				// 4. Verify Timing Records
				if len(res.Timing.Records) != len(s.records) {
					t.Fatalf("step %d: got %d timing records, want %d: %+v", stepIdx, len(res.Timing.Records), len(s.records), res.Timing.Records)
				}
				for i, rec := range s.records {
					gotRec := res.Timing.Records[i]
					switch rec.kind {
					case "pes", "rap":
						wantType, p := mediafacts.TimingRecordTypePES, gotRec.PES
						if rec.kind == "rap" {
							wantType, p = mediafacts.TimingRecordTypeRandomAccessPoint, gotRec.RAP
						}
						if gotRec.Type != wantType {
							t.Errorf("step %d rec %d: got type %v, want %s", stepIdx, i, gotRec.Type, rec.kind)
							continue
						}
						if p.Epoch != mediafacts.TimelineEpoch(rec.epoch) || p.PID != rec.pid ||
							p.HasPTS != rec.hasPTS || (rec.hasPTS && p.PTS90k != rec.pts) ||
							p.HasDTS != rec.hasDTS || (rec.hasDTS && p.DTS90k != rec.dts) ||
							p.ObservedAt != rec.obs || p.SubjectAt != rec.sub {
							t.Errorf("step %d rec %d %s mismatch: got %+v, want %+v", stepIdx, i, rec.kind, p, rec)
						}
					case "pcr":
						if gotRec.Type != mediafacts.TimingRecordTypePCR {
							t.Errorf("step %d rec %d: got type %v, want pcr", stepIdx, i, gotRec.Type)
							continue
						}
						p := gotRec.PCR
						if p.Epoch != mediafacts.TimelineEpoch(rec.epoch) || p.PCRPID != rec.pid ||
							p.ObservedAt != rec.obs || p.ExtendedPCR27m != rec.pcr {
							t.Errorf("step %d rec %d PCR mismatch: got %+v, want %+v", stepIdx, i, p, rec)
						}
					case "disc":
						if gotRec.Type != mediafacts.TimingRecordTypeDiscontinuity {
							t.Errorf("step %d rec %d: got type %v, want disc", stepIdx, i, gotRec.Type)
							continue
						}
						d := gotRec.Discontinuity
						scopeStr := "program"
						if d.Scope == mediafacts.DiscontinuityScopeTrack {
							scopeStr = fmt.Sprintf("track:%d", d.TrackPID)
						}
						if scopeStr != rec.scope {
							t.Errorf("step %d rec %d scope: got %s, want %s", stepIdx, i, scopeStr, rec.scope)
						}
						if d.ObservedAt != rec.obs {
							t.Errorf("step %d rec %d observed_at: got %d, want %d", stepIdx, i, d.ObservedAt, rec.obs)
						}
						if d.HasEpochBefore != rec.hasBefore || (rec.hasBefore && int64(d.EpochBefore) != rec.epochBefore) {
							t.Errorf("step %d rec %d epoch_before: got %v/%d, want %v/%d", stepIdx, i, d.HasEpochBefore, d.EpochBefore, rec.hasBefore, rec.epochBefore)
						}
						if d.HasEpochAfter != rec.hasAfter || (rec.hasAfter && int64(d.EpochAfter) != rec.epochAfter) {
							t.Errorf("step %d rec %d epoch_after: got %v/%d, want %v/%d", stepIdx, i, d.HasEpochAfter, d.EpochAfter, rec.hasAfter, rec.epochAfter)
						}
					}
				}
			}
		})
	}
}
