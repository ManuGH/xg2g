// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"bytes"
	"errors"
	"sync"
)

const (
	TSPacketSize = 188
	SyncByte     = 0x47
)

var (
	ErrRingClosed        = errors.New("master ring buffer closed")
	ErrInvalidPacketSize = errors.New("data slice is not 188-byte packet aligned")
	ErrNoKeyframeFound   = errors.New("no keyframe index found in buffer")
	ErrSubscriberOverrun = errors.New("subscriber was overtaken by master ring write head")
)

// VideoCodec identifies the elementary video stream codec parsed from PMT.
type VideoCodec string

const (
	CodecUnknown VideoCodec = "unknown"
	CodecH264    VideoCodec = "h264"
	CodecH265    VideoCodec = "h265"
	CodecMPEG2   VideoCodec = "mpeg2"
)

var mpeg2CRCTable [256]uint32

func init() {
	for i := 0; i < 256; i++ {
		crc := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if (crc & 0x80000000) != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
		mpeg2CRCTable[i] = crc
	}
}

// CalculateMPEG2CRC32 calculates the standard ISO/IEC 13818-1 32-bit CRC.
func CalculateMPEG2CRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc << 8) ^ mpeg2CRCTable[byte(crc>>24)^b]
	}
	return crc
}

type psiStreamAssembler struct {
	buf        []byte
	sectionLen int
	lastCC     uint8
	hasCC      bool
	lastPacket []byte
	rawPackets [][]byte
}

func (s *psiStreamAssembler) reset() {
	s.buf = s.buf[:0]
	s.sectionLen = 0
	s.hasCC = false
	s.lastPacket = nil
	s.rawPackets = s.rawPackets[:0]
}

type tableSectionTracker struct {
	inFlightVersion uint8
	hasInFlight     bool
	lastSectionNum  uint8
	sections        map[uint8][]byte
	rawPackets      map[uint8][][]byte
}

func (t *tableSectionTracker) reset() {
	t.hasInFlight = false
	t.lastSectionNum = 0
	t.sections = make(map[uint8][]byte)
	t.rawPackets = make(map[uint8][][]byte)
}

func (t *tableSectionTracker) addSection(version uint8, sectionNum uint8, lastSectionNum uint8, sectionBytes []byte, packets [][]byte) bool {
	if !t.hasInFlight || t.inFlightVersion != version || t.lastSectionNum != lastSectionNum {
		t.inFlightVersion = version
		t.hasInFlight = true
		t.lastSectionNum = lastSectionNum
		t.sections = make(map[uint8][]byte)
		t.rawPackets = make(map[uint8][][]byte)
	}

	t.sections[sectionNum] = cloneSlice(sectionBytes)
	t.rawPackets[sectionNum] = cloneSliceList(packets)

	if len(t.sections) == int(lastSectionNum)+1 {
		for i := uint8(0); i <= lastSectionNum; i++ {
			if _, ok := t.sections[i]; !ok {
				return false
			}
		}
		return true
	}
	return false
}

// MasterRing is a thread-safe, multi-reader circular FIFO buffer for MPEG-TS streams.
// It maintains an in-band, stateful index of PAT, PMT, and PES/IDR Keyframe byte offsets.
type MasterRing struct {
	mu       sync.Mutex
	notEmpty *sync.Cond
	buf      []byte
	capacity int
	head     int64 // total bytes written monotonically
	tail     int64 // oldest valid byte offset in buffer
	isClosed bool

	// PSI Table Assembly & Selection
	targetProgramNumber uint16
	patAssembler        psiStreamAssembler
	pmtAssembler        psiStreamAssembler
	patTracker          tableSectionTracker
	pmtTracker          tableSectionTracker
	rawPATPackets       [][]byte
	rawPMTPackets       [][]byte
	hasPATVersion       bool
	patVersion          uint8
	hasPMTVersion       bool
	pmtVersion          uint8
	pmtProgramNumber    uint16
	pmtPID              uint16
	videoPID            uint16
	videoCodec          VideoCodec

	// Stateful Video Parsing & Keyframe Indexing
	currentPESOffset int64
	pesHasKeyframe   bool
	pesHasSPS        bool
	pesHasPPS        bool
	pesHasVPS        bool
	annexBState      uint32 // 4-byte shift register for startcode scanning
	expectingNALByte bool
	keyframeOffsets  []int64
	maxKeyframes     int
	generation       uint64
}

// NewMasterRing creates a new MasterRing with the specified capacity (aligned to 188 bytes).
func NewMasterRing(capacityBytes int) *MasterRing {
	return NewMasterRingWithProgram(capacityBytes, 0)
}

// NewMasterRingWithProgram creates a new MasterRing targeting a specific program number in multi-program PATs.
func NewMasterRingWithProgram(capacityBytes int, targetProgram uint16) *MasterRing {
	capacityBytes = (capacityBytes / TSPacketSize) * TSPacketSize
	if capacityBytes < TSPacketSize*5 {
		capacityBytes = TSPacketSize * 5 // min 5 packets (~940 bytes)
	}

	r := &MasterRing{
		buf:                 make([]byte, capacityBytes),
		capacity:            capacityBytes,
		targetProgramNumber: targetProgram,
		videoCodec:          CodecUnknown,
		currentPESOffset:    -1,
		maxKeyframes:        64,
		annexBState:         0xFFFFFFFF,
	}
	r.patTracker.reset()
	r.pmtTracker.reset()
	r.notEmpty = sync.NewCond(&r.mu)
	return r
}

// SetTargetProgram configures the desired program number for PMT resolution,
// immediately invalidating existing PSI and decoder states if the target changed.
func (r *MasterRing) SetTargetProgram(progNum uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.targetProgramNumber != progNum {
		r.targetProgramNumber = progNum
		r.hasPATVersion = false
		r.hasPMTVersion = false
		r.pmtPID = 0
		r.patAssembler.reset()
		r.pmtAssembler.reset()
		r.patTracker.reset()
		r.pmtTracker.reset()
		r.rawPATPackets = nil
		r.invalidateVideoStateLocked()
	}
}

// Push writes a chunk of TS packets into the ring buffer and indexes PAT/PMT/IDR boundaries.
func (r *MasterRing) Push(data []byte) (int, error) {
	if len(data)%TSPacketSize != 0 {
		return 0, ErrInvalidPacketSize
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isClosed {
		return 0, ErrRingClosed
	}

	n := len(data)
	if n == 0 {
		return 0, nil
	}

	// 1. Parse and index each 188-byte packet monotonically
	for i := 0; i < n; i += TSPacketSize {
		pkt := data[i : i+TSPacketSize]
		pktOffset := r.head + int64(i)
		r.indexPacketLocked(pkt, pktOffset)
	}

	// 2. Write data into circular buffer safely, supporting len(data) > capacity without panic
	remaining := data
	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > r.capacity {
			chunk = chunk[:r.capacity]
		}
		remaining = remaining[len(chunk):]

		chunkLen := len(chunk)
		writePos := int(r.head % int64(r.capacity))
		firstChunk := r.capacity - writePos
		if chunkLen <= firstChunk {
			copy(r.buf[writePos:], chunk)
		} else {
			copy(r.buf[writePos:], chunk[:firstChunk])
			copy(r.buf[:chunkLen-firstChunk], chunk[firstChunk:])
		}

		r.head += int64(chunkLen)
		if r.head-r.tail > int64(r.capacity) {
			r.tail = r.head - int64(r.capacity)
			r.pruneKeyframesLocked()
		}
	}

	r.notEmpty.Broadcast()
	return n, nil
}

func (r *MasterRing) indexPacketLocked(pkt []byte, offset int64) {
	if len(pkt) < TSPacketSize || pkt[0] != SyncByte {
		return
	}

	pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
	pusi := (pkt[1] & 0x40) != 0
	afc := (pkt[3] >> 4) & 0x03
	hasPayload := (afc == 0x01 || afc == 0x03)
	if !hasPayload {
		return
	}

	payloadOffset := 4
	if afc == 0x03 {
		afLen := int(pkt[4])
		payloadOffset = 5 + afLen
		if payloadOffset >= TSPacketSize {
			return
		}
	}
	payload := pkt[payloadOffset:]

	// 1. PAT (PID 0) Section Assembly
	if pid == 0 {
		r.feedPSIPacketLocked(true, pkt, pusi, payload)
		return
	}

	// 2. PMT Section Assembly
	if r.pmtPID > 0 && pid == r.pmtPID {
		r.feedPSIPacketLocked(false, pkt, pusi, payload)
		return
	}

	// 3. Video Elementary Stream Keyframe / IDR Indexing
	if r.videoPID > 0 && pid == r.videoPID {
		r.parseVideoPacketLocked(pkt, offset, pusi, payload)
	}
}

func (r *MasterRing) feedBytesToAssemblerLocked(isPAT bool, assembler *psiStreamAssembler, chunk []byte, pkt []byte) int {
	if len(chunk) == 0 {
		return 0
	}

	consumed := 0

	// Phase 1: Complete 3-byte header if incomplete
	if len(assembler.buf) < 3 {
		needHeader := 3 - len(assembler.buf)
		toTake := len(chunk)
		if toTake > needHeader {
			toTake = needHeader
		}
		assembler.buf = append(assembler.buf, chunk[:toTake]...)
		assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
		chunk = chunk[toTake:]
		consumed += toTake

		if len(assembler.buf) >= 3 {
			secLen := int((uint16(assembler.buf[1]&0x0F) << 8) | uint16(assembler.buf[2]))
			assembler.sectionLen = secLen + 3
		} else {
			return consumed
		}
	}

	// Phase 2: Complete body up to sectionLen
	if assembler.sectionLen > 0 && len(assembler.buf) < assembler.sectionLen {
		needed := assembler.sectionLen - len(assembler.buf)
		toTake := len(chunk)
		if toTake > needed {
			toTake = needed
		}
		assembler.buf = append(assembler.buf, chunk[:toTake]...)
		if consumed == 0 { // Not added in Phase 1 for this packet
			assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
		}
		consumed += toTake

		if len(assembler.buf) >= assembler.sectionLen {
			r.processCompletePSISectionLocked(isPAT, assembler.buf[:assembler.sectionLen], assembler.rawPackets)
			assembler.buf = assembler.buf[:0]
			assembler.sectionLen = 0
			assembler.rawPackets = assembler.rawPackets[:0]
		}
	}

	return consumed
}

func (r *MasterRing) feedPSIPacketLocked(isPAT bool, pkt []byte, pusi bool, payload []byte) {
	var assembler *psiStreamAssembler
	if isPAT {
		assembler = &r.patAssembler
	} else {
		assembler = &r.pmtAssembler
	}

	cc := pkt[3] & 0x0F
	if assembler.hasCC {
		if cc == assembler.lastCC {
			if bytes.Equal(pkt, assembler.lastPacket) {
				// Exact byte-for-byte duplicate TS packet: silently ignore
				return
			}
			// Same CC but different content: glitch / discontinuity!
			assembler.reset()
			if !pusi {
				return
			}
		} else if cc != (assembler.lastCC+1)&0x0F {
			// Continuity gap detected: abort corrupted in-flight assembly
			assembler.reset()
			if !pusi {
				return
			}
		}
	}
	assembler.lastCC = cc
	assembler.hasCC = true
	assembler.lastPacket = cloneSlice(pkt)

	offset := 0

	if pusi {
		if len(payload) < 1 {
			return
		}
		pointerField := int(payload[0])
		offset = 1

		if pointerField > 0 {
			// Case A: Bytes before pointer complete previous in-flight section
			if len(assembler.buf) > 0 {
				toTake := pointerField
				if 1+toTake > len(payload) {
					assembler.reset()
					return
				}
				r.feedBytesToAssemblerLocked(isPAT, assembler, payload[1:1+toTake], pkt)
				if len(assembler.buf) > 0 {
					// Still incomplete after pointer field: missing data, discard
					assembler.buf = assembler.buf[:0]
					assembler.sectionLen = 0
					assembler.rawPackets = assembler.rawPackets[:0]
				}
			}
			offset = 1 + pointerField
		} else {
			// Case B: pointerField == 0. Discard any unfinished prior section
			assembler.buf = assembler.buf[:0]
			assembler.sectionLen = 0
			assembler.rawPackets = assembler.rawPackets[:0]
			offset = 1
		}
	} else {
		// pusi == false: continue in-flight section
		if len(assembler.buf) > 0 {
			consumed := r.feedBytesToAssemblerLocked(isPAT, assembler, payload, pkt)
			offset = consumed
		} else {
			return
		}
	}

	// Parse subsequent sections in this payload (supporting multiple sections per packet)
	for offset < len(payload) {
		if payload[offset] == 0xFF {
			// MPEG-TS PSI padding / stuffing bytes reached
			break
		}

		avail := len(payload) - offset
		if avail < 3 {
			// Fragmented section header (1-2 bytes) spanning into next packet!
			assembler.buf = append(assembler.buf, payload[offset:]...)
			assembler.sectionLen = 0
			assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
			break
		}

		tableID := payload[offset]
		expectedTableID := byte(0x00)
		if !isPAT {
			expectedTableID = 0x02
		}
		if tableID != expectedTableID {
			break
		}

		secLen := int((uint16(payload[offset+1]&0x0F) << 8) | uint16(payload[offset+2]))
		fullSecLen := secLen + 3

		if avail >= fullSecLen {
			// Complete section self-contained in this payload chunk!
			sectionBytes := payload[offset : offset+fullSecLen]
			r.processCompletePSISectionLocked(isPAT, sectionBytes, [][]byte{cloneSlice(pkt)})
			offset += fullSecLen
			continue
		}

		// Section spans across to next TS packet
		assembler.buf = append(assembler.buf, payload[offset:]...)
		assembler.sectionLen = fullSecLen
		assembler.rawPackets = append(assembler.rawPackets, cloneSlice(pkt))
		break
	}
}

func (r *MasterRing) processCompletePSISectionLocked(isPAT bool, table []byte, rawPackets [][]byte) {
	if len(table) < 12 {
		return
	}

	// Validate MPEG-2 CRC32
	if CalculateMPEG2CRC32(table) != 0 {
		return
	}

	currentNext := table[5] & 0x01
	if currentNext != 1 {
		return
	}

	version := (table[5] >> 1) & 0x1F
	sectionNum := table[6]
	lastSectionNum := table[7]

	if isPAT {
		tableComplete := r.patTracker.addSection(version, sectionNum, lastSectionNum, table, rawPackets)
		if !tableComplete {
			return
		}

		// Full PAT Table Generation Complete: scan all sections for target program
		matchedPID := uint16(0)
		for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
			sData := r.patTracker.sections[sIdx]
			if len(sData) < 12 {
				continue
			}
			programs := sData[8 : len(sData)-4]
			for i := 0; i+4 <= len(programs); i += 4 {
				progNum := (uint16(programs[i]) << 8) | uint16(programs[i+1])
				progPID := ((uint16(programs[i+2]) & 0x1F) << 8) | uint16(programs[i+3])
				if progNum == 0 {
					continue
				}

				if r.targetProgramNumber > 0 {
					if progNum == r.targetProgramNumber {
						matchedPID = progPID
						break
					}
				} else {
					matchedPID = progPID
					break
				}
			}
			if matchedPID > 0 && r.targetProgramNumber > 0 {
				break
			}
		}

		if matchedPID > 0 {
			if !r.hasPMTVersion || r.pmtPID != matchedPID || !r.hasPATVersion || r.patVersion != version {
				if r.pmtPID != matchedPID {
					r.pmtPID = matchedPID
					r.pmtAssembler.reset()
					r.pmtTracker.reset()
					r.hasPMTVersion = false
					r.invalidateVideoStateLocked()
				}
			}
			r.hasPATVersion = true
			r.patVersion = version

			// Build deduplicated rawPATPackets from all sections 0..lastSectionNum
			var allPATPackets [][]byte
			for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
				for _, pkt := range r.patTracker.rawPackets[sIdx] {
					if !containsPacket(allPATPackets, pkt) {
						allPATPackets = append(allPATPackets, cloneSlice(pkt))
					}
				}
			}
			r.rawPATPackets = allPATPackets
		}
	} else {
		progNum := (uint16(table[3]) << 8) | uint16(table[4])
		tableComplete := r.pmtTracker.addSection(version, sectionNum, lastSectionNum, table, rawPackets)
		if !tableComplete {
			return
		}

		// Full PMT Table Generation Complete
		isChanged := !r.hasPMTVersion || version != r.pmtVersion || progNum != r.pmtProgramNumber
		if isChanged {
			r.hasPMTVersion = true
			r.pmtVersion = version
			r.pmtProgramNumber = progNum
			r.invalidateVideoStateLocked()

			// Scan elementary streams across all sections 0..lastSectionNum
			for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
				sData := r.pmtTracker.sections[sIdx]
				if len(sData) < 12 {
					continue
				}
				progInfoLen := int((uint16(sData[10]&0x0F) << 8) | uint16(sData[11]))
				esStart := 12 + progInfoLen
				esEnd := len(sData) - 4

				for i := esStart; i+5 <= esEnd && i < len(sData); {
					st := sData[i]
					elemPID := ((uint16(sData[i+1]) & 0x1F) << 8) | uint16(sData[i+2])
					esInfoLen := int((uint16(sData[i+3]&0x0F) << 8) | uint16(sData[i+4]))

					switch st {
					case 0x1B: // H.264 / AVC
						r.videoPID = elemPID
						r.videoCodec = CodecH264
					case 0x24: // H.265 / HEVC
						r.videoPID = elemPID
						r.videoCodec = CodecH265
					case 0x02, 0x01: // MPEG-2 / MPEG-1 Video
						r.videoPID = elemPID
						r.videoCodec = CodecMPEG2
					}

					if r.videoPID > 0 {
						break
					}
					i += 5 + esInfoLen
				}
				if r.videoPID > 0 {
					break
				}
			}
		}

		// Build deduplicated rawPMTPackets from all sections 0..lastSectionNum
		var allPMTPackets [][]byte
		for sIdx := uint8(0); sIdx <= lastSectionNum; sIdx++ {
			for _, pkt := range r.pmtTracker.rawPackets[sIdx] {
				if !containsPacket(allPMTPackets, pkt) {
					allPMTPackets = append(allPMTPackets, cloneSlice(pkt))
				}
			}
		}
		r.rawPMTPackets = allPMTPackets
	}
}

func containsPacket(list [][]byte, pkt []byte) bool {
	for _, existing := range list {
		if bytes.Equal(existing, pkt) {
			return true
		}
	}
	return false
}

func (r *MasterRing) invalidateVideoStateLocked() {
	r.videoPID = 0
	r.videoCodec = CodecUnknown
	r.currentPESOffset = -1
	r.pesHasKeyframe = false
	r.pesHasSPS = false
	r.pesHasPPS = false
	r.pesHasVPS = false
	r.annexBState = 0xFFFFFFFF
	r.expectingNALByte = false
	r.keyframeOffsets = r.keyframeOffsets[:0]
	r.rawPMTPackets = nil
	r.generation++
}

func (r *MasterRing) parseVideoPacketLocked(pkt []byte, offset int64, pusi bool, payload []byte) {
	esData := payload

	if pusi {
		// Video PES packet start: verify PES startcode prefix (00 00 01 E0..EF)
		if len(payload) >= 9 && payload[0] == 0x00 && payload[1] == 0x00 && payload[2] == 0x01 && (payload[3] >= 0xE0 && payload[3] <= 0xEF) {
			r.currentPESOffset = offset
			r.pesHasKeyframe = false
			r.pesHasSPS = false
			r.pesHasPPS = false
			r.pesHasVPS = false
			r.annexBState = 0xFFFFFFFF
			r.expectingNALByte = false

			pesHeaderDataLen := int(payload[8])
			esStart := 9 + pesHeaderDataLen
			if esStart < len(payload) {
				esData = payload[esStart:]
			} else {
				esData = nil
			}
		}
	}

	if len(esData) == 0 {
		return
	}

	// Stateful Annex-B NAL unit parser across packet boundaries
	for _, b := range esData {
		r.annexBState = (r.annexBState << 8) | uint32(b)

		if r.expectingNALByte {
			r.expectingNALByte = false
			isKeyframe := false

			if r.videoCodec == CodecH264 {
				nalType := b & 0x1F
				switch nalType {
				case 5: // IDR Slice
					isKeyframe = true
				case 7: // SPS
					r.pesHasSPS = true
				case 8: // PPS
					r.pesHasPPS = true
				case 1, 2: // VCL Slice
					if r.pesHasSPS && r.pesHasPPS {
						// Complete DVB Recovery / Open-GOP Random Access Point with SPS+PPS
						isKeyframe = true
					}
				}
			} else if r.videoCodec == CodecH265 {
				nalType := (b >> 1) & 0x3F
				switch {
				case nalType == 19 || nalType == 20 || nalType == 21: // IDR_W_RADL, IDR_N_LP, CRA_NUT
					isKeyframe = true
				case nalType == 32: // VPS
					r.pesHasVPS = true
				case nalType == 33: // SPS
					r.pesHasSPS = true
				case nalType == 34: // PPS
					r.pesHasPPS = true
				case nalType == 1: // Trailing / non-IDR slice
					if r.pesHasVPS && r.pesHasSPS && r.pesHasPPS {
						isKeyframe = true
					}
				}
			}

			if isKeyframe && !r.pesHasKeyframe && r.currentPESOffset >= 0 {
				r.pesHasKeyframe = true
				r.keyframeOffsets = append(r.keyframeOffsets, r.currentPESOffset)
				if len(r.keyframeOffsets) > r.maxKeyframes {
					r.keyframeOffsets = r.keyframeOffsets[1:]
				}
			}
		}

		// Check for 3-byte (00 00 01) or 4-byte (00 00 00 01) Annex-B startcode
		if (r.annexBState & 0x00FFFFFF) == 0x00000001 {
			r.expectingNALByte = true
		}
	}
}

func (r *MasterRing) pruneKeyframesLocked() {
	validIdx := -1
	for i, offset := range r.keyframeOffsets {
		if offset >= r.tail {
			validIdx = i
			break
		}
	}
	if validIdx == -1 {
		r.keyframeOffsets = r.keyframeOffsets[:0]
	} else if validIdx > 0 {
		r.keyframeOffsets = r.keyframeOffsets[validIdx:]
	}
}

// PrimedAttachPoint represents an atomic, generation-locked stream entry point.
type PrimedAttachPoint struct {
	Preamble       []byte
	KeyframeOffset int64
	Generation     uint64
	HasKeyframe    bool
}

// PrimedAttachPoint captures an atomic snapshot of the active PAT/PMT preamble,
// the latest valid keyframe offset, and the active stream generation under a single lock.
func (r *MasterRing) PrimedAttachPoint() PrimedAttachPoint {
	r.mu.Lock()
	defer r.mu.Unlock()

	var preamble []byte
	for _, pkt := range r.rawPATPackets {
		preamble = append(preamble, pkt...)
	}
	for _, pkt := range r.rawPMTPackets {
		preamble = append(preamble, pkt...)
	}

	var kfOffset int64
	var hasKf bool
	if len(r.keyframeOffsets) > 0 {
		latest := r.keyframeOffsets[len(r.keyframeOffsets)-1]
		if latest >= r.tail {
			kfOffset = latest
			hasKf = true
		}
	}

	return PrimedAttachPoint{
		Preamble:       preamble,
		KeyframeOffset: kfOffset,
		Generation:     r.generation,
		HasKeyframe:    hasKf,
	}
}

// LatestKeyframeOffset returns the absolute byte offset of the most recent valid keyframe.
func (r *MasterRing) LatestKeyframeOffset() (int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.keyframeOffsets) == 0 {
		return 0, false
	}
	latest := r.keyframeOffsets[len(r.keyframeOffsets)-1]
	if latest < r.tail {
		return 0, false
	}
	return latest, true
}

// PATPMTPreamble returns concatenated raw PAT and PMT TS packets across all active sections.
func (r *MasterRing) PATPMTPreamble() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	var preamble []byte
	for _, pkt := range r.rawPATPackets {
		preamble = append(preamble, pkt...)
	}
	for _, pkt := range r.rawPMTPackets {
		preamble = append(preamble, pkt...)
	}
	return preamble
}

// VideoDetails returns authoritative video PID and Codec discovered from PMT.
func (r *MasterRing) VideoDetails() (uint16, VideoCodec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.videoPID, r.videoCodec
}

// Head returns total bytes written monotonically.
func (r *MasterRing) Head() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.head
}

// Tail returns the oldest valid byte offset.
func (r *MasterRing) Tail() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tail
}

// BufferedBytes returns total valid unpruned bytes in the ring.
func (r *MasterRing) BufferedBytes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int(r.head - r.tail)
}

// Close closes the master ring buffer, waking all blocked subscriber readers.
func (r *MasterRing) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.isClosed = true
	r.notEmpty.Broadcast()
}

func cloneSlice(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneSliceList(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i, s := range in {
		out[i] = cloneSlice(s)
	}
	return out
}
