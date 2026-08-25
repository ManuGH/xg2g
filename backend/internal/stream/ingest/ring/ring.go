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

	// scrambledVerdictMinPackets is the number of scrambled video packets that must be observed,
	// with zero clear ones, before the stream is declared scrambled. At broadcast video rates this
	// threshold is crossed within a few tens of milliseconds.
	scrambledVerdictMinPackets = 100
)

var (
	ErrRingClosed        = errors.New("master ring buffer closed")
	ErrInvalidPacketSize = errors.New("data slice is not 188-byte packet aligned")
	ErrNoKeyframeFound   = errors.New("no keyframe index found in buffer")
	ErrSubscriberOverrun = errors.New("subscriber was overtaken by master ring write head")
	// ErrScrambledStream reports that the upstream video elementary stream carries a non-zero
	// transport_scrambling_control field. Encrypted payload can never yield a valid Annex-B
	// keyframe, so waiting for one is futile: the receiver is not descrambling this service.
	ErrScrambledStream = errors.New("upstream video elementary stream is scrambled; receiver descrambling unavailable")
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
	// Counted as uint32 so the shift needs no narrowing conversion; the bound
	// makes the two identical, but only one of them is checkable.
	for i := uint32(0); i < 256; i++ {
		crc := i << 24
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

	// Random access classification for the access unit currently being assembled.
	// Held per access unit because joinability is a property of all of its slices,
	// which is only known once the next access unit begins.
	auHasIRAP       bool
	auHasRecoveryPt bool
	auVCLCount      int
	auIntraVCLCount int
	// auScrambledPackets counts scrambled packets inside the access unit being
	// assembled. An entry point whose own access unit was partly encrypted is not
	// one a decoder can be started on.
	auScrambledPackets int
	cleanRAPCount      uint64
	cleanAccessUnits   uint64
	nalBuf             []byte
	nalKind            nalCaptureKind
	nalLeft            int
	// nalSkip is the number of bytes of the NAL header still to pass over before
	// the capture starts. H.264 has a one byte header, which is already consumed by
	// the classification step; HEVC has two, and capturing from the first of the
	// remaining bytes would read nuh_layer_id as if it were an SEI payload type.
	nalSkip int
	raObs   RandomAccessObservation

	// Scrambling observation, kept per elementary stream rather than as one total.
	// A service whose video descrambles while its audio does not is a different
	// fault from one where neither does, and a single counter cannot tell them apart.
	scrambledVideoPackets uint64
	clearVideoPackets     uint64
	scrambledAudioPackets uint64
	clearAudioPackets     uint64
	// videoClearRun counts consecutive clear video packets, reset by any scrambled
	// one. Descrambling on this receiver was measured coming up intermittently -
	// 733 scrambled packets interleaved with clear ones on one tune - so "a clear
	// packet was seen" is not evidence that the stream is clear.
	videoClearRun uint64
	// audioClearRun is the same measure on the audio streams, kept separately so a
	// service whose video is through but whose audio is not can be told apart.
	audioClearRun uint64
	// audioPIDs holds the elementary audio streams named by the active PMT.
	audioPIDs   []uint16
	audioTracks []AudioTrackInfo
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
		return
	}

	// 4. Audio elementary streams, observed for descrambling only. Their payload is
	//    never parsed here - the client selects and decodes the track - but whether
	//    it arrives clear decides whether a channel is presentable.
	for _, apid := range r.audioPIDs {
		if pid == apid {
			if (pkt[3]>>6)&0x03 != 0 {
				r.scrambledAudioPackets++
				r.audioClearRun = 0
			} else {
				r.clearAudioPackets++
				r.audioClearRun++
			}
			return
		}
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
						if r.videoPID == 0 {
							r.videoPID = elemPID
							r.videoCodec = CodecH264
						}
					case 0x24, 0x27: // H.265 / HEVC
						if r.videoPID == 0 {
							r.videoPID = elemPID
							r.videoCodec = CodecH265
						}
					case 0x02, 0x01: // MPEG-2 / MPEG-1 Video
						if r.videoPID == 0 {
							r.videoPID = elemPID
							r.videoCodec = CodecMPEG2
						}
					default:
						// The elementary stream loop is now walked to its end rather
						// than stopped at the video entry: the audio streams that
						// follow it are needed to tell "video descrambled, audio did
						// not" apart from "nothing descrambled".
						descriptors := []byte(nil)
						if dStart := i + 5; dStart+esInfoLen <= len(sData) {
							descriptors = sData[dStart : dStart+esInfoLen]
						}
						if isAudioStreamType(st, descriptors) {
							r.audioPIDs = appendPID(r.audioPIDs, elemPID)
							codec := AudioCodecFromStreamType(st, descriptors)
							lang := LanguageFromDescriptors(descriptors)
							r.audioTracks = appendAudioTrack(r.audioTracks, AudioTrackInfo{
								PID:        elemPID,
								StreamType: st,
								Codec:      codec,
								Language:   lang,
							})
						}
					}

					i += 5 + esInfoLen
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

func appendAudioTrack(list []AudioTrackInfo, track AudioTrackInfo) []AudioTrackInfo {
	for i, existing := range list {
		if existing.PID == track.PID {
			list[i] = track
			return list
		}
	}
	return append(list, track)
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
	r.scrambledVideoPackets = 0
	r.clearVideoPackets = 0
	r.scrambledAudioPackets = 0
	r.clearAudioPackets = 0
	r.videoClearRun = 0
	r.audioClearRun = 0
	r.audioPIDs = nil
	r.audioTracks = nil
	r.raObs = RandomAccessObservation{}
	r.cleanRAPCount = 0
	r.cleanAccessUnits = 0
	r.resetAccessUnitStateLocked()
	r.generation++
}

func (r *MasterRing) parseVideoPacketLocked(pkt []byte, offset int64, pusi bool, payload []byte) {
	// transport_scrambling_control != 0 means the payload is encrypted. Feeding it to the
	// Annex-B scanner would index random bytes as NAL units, so it is never parsed. The
	// observation is recorded instead, allowing attach to fail fast with ErrScrambledStream.
	if (pkt[3]>>6)&0x03 != 0 {
		r.scrambledVideoPackets++
		r.auScrambledPackets++
		r.videoClearRun = 0
		return
	}
	r.clearVideoPackets++
	r.videoClearRun++

	esData := payload

	if pusi {
		// Video PES packet start: verify PES startcode prefix (00 00 01 E0..EF)
		if len(payload) >= 9 && payload[0] == 0x00 && payload[1] == 0x00 && payload[2] == 0x01 && (payload[3] >= 0xE0 && payload[3] <= 0xEF) {
			// Whether an access unit is joinable depends on every slice in it, which
			// is only known once it ends - and it ends where the next one begins.
			r.finalizeAccessUnitLocked()

			r.currentPESOffset = offset
			r.pesHasKeyframe = false
			r.pesHasSPS = false
			r.pesHasPPS = false
			r.pesHasVPS = false
			r.annexBState = 0xFFFFFFFF
			r.expectingNALByte = false
			r.resetAccessUnitStateLocked()

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

	// Stateful Annex-B NAL unit parser across packet boundaries.
	for _, b := range esData {
		r.annexBState = (r.annexBState << 8) | uint32(b)

		// Bytes immediately following a NAL header, held for slice-header and SEI
		// parsing. Collected here because a NAL unit routinely spans TS packets and
		// the fields that matter sit in its first bytes.
		if r.nalLeft > 0 {
			if r.nalSkip > 0 {
				r.nalSkip--
			} else {
				r.nalBuf = append(r.nalBuf, b)
				r.nalLeft--
				if r.nalLeft == 0 {
					r.consumeNALCaptureLocked()
				}
			}
		}

		if r.expectingNALByte {
			r.expectingNALByte = false
			// A start code inside a captured NAL means the capture budget outran the
			// unit; read what was collected before moving on.
			r.consumeNALCaptureLocked()

			switch r.videoCodec {
			case CodecH264:
				r.classifyH264NALLocked(b & 0x1F)
			case CodecH265:
				r.classifyHEVCNALLocked((b >> 1) & 0x3F)
			case CodecMPEG2:
				r.classifyMPEG2StartCodeLocked(b)
			}
		}

		// Check for 3-byte (00 00 01) or 4-byte (00 00 00 01) Annex-B startcode
		if (r.annexBState & 0x00FFFFFF) == 0x00000001 {
			r.expectingNALByte = true
		}
	}
}

// classifyMPEG2StartCodeLocked records what an MPEG-2 Video Elementary Stream start code
// contributes to the current access unit and stream configuration.
// ISO/IEC 13818-2:
// - 0xB3: sequence_header_code (SPS equivalent / Codec Config)
// - 0xB8: group_start_code (GOP Header / Entry Point)
// - 0x00: picture_start_code (Picture Header -> contains picture_coding_type I/P/B)
func (r *MasterRing) classifyMPEG2StartCodeLocked(startCode uint8) {
	switch startCode {
	case 0xB3: // Sequence Header
		r.pesHasSPS = true
	case 0xB8: // Group of Pictures Header
		r.pesHasVPS = true
	case 0x00: // Picture Header
		r.auVCLCount++
		r.beginNALCaptureLocked(captureMPEG2PictureHeader, mpeg2PictureHeaderCaptureBytes, 0)
	}
}

// classifyH264NALLocked records what one H.264 NAL unit contributes to the access
// unit being assembled, and starts a capture where the following bytes are needed.
func (r *MasterRing) classifyH264NALLocked(nalType uint8) {
	switch nalType {
	case h264NALSliceIDR:
		r.auHasIRAP = true
		r.auVCLCount++
		r.auIntraVCLCount++
		// An IDR needs nothing else to be joinable, so it is indexed without waiting
		// for the access unit to end. This keeps streams that do emit IDRs attaching
		// exactly as fast as before.
		r.indexRandomAccessPointLocked(true)
	case h264NALSliceNonIDR, h264NALSlicePartA:
		r.auVCLCount++
		r.beginNALCaptureLocked(captureH264SliceHeader, sliceHeaderCaptureBytes, 0)
	case h264NALSPS:
		r.pesHasSPS = true
	case h264NALPPS:
		r.pesHasPPS = true
	case h264NALSEI:
		// The one byte H.264 NAL header has already been consumed, so the SEI
		// payload starts with the very next byte.
		r.beginNALCaptureLocked(captureSEI, seiCaptureBytes, 0)
	}
}

// classifyHEVCNALLocked mirrors classifyH264NALLocked for HEVC.
//
// The whole IRAP range qualifies, not only IDR: a CRA or BLA picture is equally a
// point a decoder can be started on. HEVC slice headers are not parsed for intra
// coding - the fields needed sit behind the active parameter sets - so a stream that
// never emits an IRAP is admitted on its recovery_point SEI instead, which is the
// stream's own declaration of a random access point.
func (r *MasterRing) classifyHEVCNALLocked(nalType uint8) {
	switch {
	case nalType >= hevcNALIRAPFirst && nalType <= hevcNALIRAPLast:
		r.auHasIRAP = true
		r.auVCLCount++
		r.auIntraVCLCount++
		r.indexRandomAccessPointLocked(true)
	case nalType <= 9:
		r.auVCLCount++
	case nalType == hevcNALVPS:
		r.pesHasVPS = true
	case nalType == hevcNALSPS:
		r.pesHasSPS = true
	case nalType == hevcNALPPS:
		r.pesHasPPS = true
	case nalType == hevcNALPrefixSEI:
		// The HEVC NAL header is two bytes. Only the first has been consumed by the
		// classification above, so the second is passed over: reading it as the SEI
		// payload type shifts every field that follows and hides the recovery point,
		// which on a stream that emits no IRAP is the only signal there is.
		r.beginNALCaptureLocked(captureSEI, seiCaptureBytes, 1)
	}
}

func (r *MasterRing) beginNALCaptureLocked(kind nalCaptureKind, budget, skipHeaderBytes int) {
	r.nalKind = kind
	r.nalLeft = budget
	r.nalSkip = skipHeaderBytes
	r.nalBuf = r.nalBuf[:0]
}

// consumeNALCaptureLocked reads whatever was captured and clears the capture.
func (r *MasterRing) consumeNALCaptureLocked() {
	if r.nalKind == captureNone || len(r.nalBuf) == 0 {
		r.nalKind = captureNone
		r.nalLeft = 0
		r.nalSkip = 0
		return
	}

	switch r.nalKind {
	case captureH264SliceHeader:
		isIntra, ok := h264SliceIsIntra(r.nalBuf)
		switch {
		case !ok:
			r.raObs.UnreadableSlices++
		case isIntra:
			r.auIntraVCLCount++
		}
	case captureSEI:
		if seiHasRecoveryPoint(r.nalBuf) {
			r.auHasRecoveryPt = true
		}
	case captureMPEG2PictureHeader:
		isIntra, ok := mpeg2PictureIsIntra(r.nalBuf)
		switch {
		case !ok:
			r.raObs.UnreadableSlices++
		case isIntra:
			r.auHasIRAP = true
			r.auIntraVCLCount++
			r.indexRandomAccessPointLocked(true)
		}
	}

	r.nalKind = captureNone
	r.nalLeft = 0
	r.nalSkip = 0
	r.nalBuf = r.nalBuf[:0]
}

// finalizeAccessUnitLocked decides whether the access unit that just ended was a
// random access point, now that all of its slices have been seen.
func (r *MasterRing) finalizeAccessUnitLocked() {
	r.consumeNALCaptureLocked()

	if r.currentPESOffset < 0 || r.auVCLCount == 0 {
		return
	}

	// Descrambling is proven by any complete picture that arrived without an
	// encrypted packet in it, not only by one a decoder could start on. Measured
	// against the receiver, tying the two together made them true at the same
	// millisecond in every zap, so the descrambling observation carried no
	// information of its own. A clear predicted picture arrives up to a GOP before
	// the next clear entry point does.
	if r.auScrambledPackets == 0 {
		r.cleanAccessUnits++
	}

	if r.pesHasKeyframe {
		return
	}

	// An access unit with no parameter sets cannot configure a decoder that is
	// starting cold, whatever its slices contain.
	hasParameterSets := r.pesHasSPS && r.pesHasPPS
	if r.videoCodec == CodecH265 {
		hasParameterSets = hasParameterSets && r.pesHasVPS
	}
	if !hasParameterSets {
		return
	}

	joinable := false
	switch r.videoCodec {
	case CodecH264:
		// Every coded slice intra means the picture stands alone. One predicted
		// slice is enough to disqualify it: the decoder would be missing exactly
		// the references that slice names.
		joinable = r.auIntraVCLCount == r.auVCLCount
	case CodecH265:
		joinable = r.auHasRecoveryPt
	}

	if !joinable {
		if r.auHasRecoveryPt || r.auVCLCount > r.auIntraVCLCount {
			r.raObs.PredictedRejected++
		}
		return
	}

	r.indexRandomAccessPointLocked(false)
}

// indexRandomAccessPointLocked records the current access unit as an attach point.
func (r *MasterRing) indexRandomAccessPointLocked(irap bool) {
	if r.pesHasKeyframe || r.currentPESOffset < 0 {
		return
	}
	r.pesHasKeyframe = true

	if irap {
		r.raObs.IRAPPoints++
	} else {
		r.raObs.IntraPoints++
	}
	if r.auHasRecoveryPt {
		r.raObs.RecoveryPointSEIs++
	}
	// An entry point whose own access unit carried scrambled packets is not one a
	// decoder can be started on, however well it classified.
	if r.auScrambledPackets == 0 {
		r.cleanRAPCount++
	}

	r.keyframeOffsets = append(r.keyframeOffsets, r.currentPESOffset)
	if len(r.keyframeOffsets) > r.maxKeyframes {
		r.keyframeOffsets = r.keyframeOffsets[1:]
	}
}

func (r *MasterRing) resetAccessUnitStateLocked() {
	r.auHasIRAP = false
	r.auHasRecoveryPt = false
	r.auVCLCount = 0
	r.auIntraVCLCount = 0
	r.auScrambledPackets = 0
	r.nalKind = captureNone
	r.nalLeft = 0
	r.nalSkip = 0
	r.nalBuf = r.nalBuf[:0]
}

// RandomAccess returns how access units have been classified on this stream.
func (r *MasterRing) RandomAccess() RandomAccessObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.raObs
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
	return r.latestKeyframeOffsetLocked()
}

// latestKeyframeOffsetLocked reports the newest random access point still held by
// the ring. A keyframe that has fallen behind the tail is gone even though its
// offset is still indexed, so it is not a valid entry point.
//
// Callers that already hold r.mu use this; the exported wrappers must not, because
// r.mu is not reentrant and SubscriberReader.Read holds it across recovery.
func (r *MasterRing) latestKeyframeOffsetLocked() (int64, bool) {
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
	return r.patpmtPreambleLocked()
}

// patpmtPreambleLocked builds the active topology preamble for callers already
// holding r.mu. See latestKeyframeOffsetLocked for why the split exists.
func (r *MasterRing) patpmtPreambleLocked() []byte {
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

// ScramblingObservation reports how many payload-carrying TS packets on the selected video PID
// were seen scrambled versus clear. Intended for diagnostics and telemetry.
func (r *MasterRing) ScramblingObservation() (scrambled uint64, clear uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scrambledVideoPackets, r.clearVideoPackets
}

// StreamScrambling reports descrambling per elementary stream.
//
// Separate counters because the two faults they distinguish need different answers:
// video clear with audio scrambled is a service the receiver is only half
// descrambling, while neither clear is a service it is not descrambling at all.
type StreamScrambling struct {
	VideoScrambled uint64
	VideoClear     uint64
	AudioScrambled uint64
	AudioClear     uint64
	// VideoClearRun is the number of clear video packets since the last scrambled
	// one. Descrambling was measured coming up intermittently on this receiver, so
	// the run length - not the total - is what says the stream is clear now.
	VideoClearRun uint64
	// AudioClearRun is the same measure across the audio streams.
	AudioClearRun uint64
	// AudioPIDs are the audio elementary streams named by the active PMT.
	AudioPIDs []uint16
}

// Scrambling returns the per-stream descrambling observation.
func (r *MasterRing) Scrambling() StreamScrambling {
	r.mu.Lock()
	defer r.mu.Unlock()
	pids := make([]uint16, len(r.audioPIDs))
	copy(pids, r.audioPIDs)
	return StreamScrambling{
		VideoScrambled: r.scrambledVideoPackets,
		VideoClear:     r.clearVideoPackets,
		AudioScrambled: r.scrambledAudioPackets,
		AudioClear:     r.clearAudioPackets,
		VideoClearRun:  r.videoClearRun,
		AudioClearRun:  r.audioClearRun,
		AudioPIDs:      pids,
	}
}

// scrambledVideoConfirmedLocked reports whether the video stream is conclusively scrambled.
// It requires a minimum sample so that a handful of packets observed mid key-change cannot
// trip the verdict, and demands that not one clear payload packet has been seen: a receiver
// that descrambles clears transport_scrambling_control on every packet it emits.
func (r *MasterRing) scrambledVideoConfirmedLocked() bool {
	return r.clearVideoPackets == 0 && r.scrambledVideoPackets >= scrambledVerdictMinPackets
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

// KeyframeOffsets returns the currently indexed random access offsets.
func (r *MasterRing) KeyframeOffsets() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.keyframeOffsets))
	copy(out, r.keyframeOffsets)
	return out
}

// ReadinessFacts is everything the ring knows that bears on whether a channel is
// presentable. It is a snapshot, taken without blocking the ingest.
//
// Deliberately facts rather than a verdict: what counts as presentable is a policy
// question that belongs one layer up, and keeping it there means the policy can be
// measured against reality before it is enforced.
type ReadinessFacts struct {
	// Generation increments whenever the PSI describing this stream changes, which
	// is what a PMT version bump or a codec change looks like from here. A consumer
	// that carries timestamps across a generation change is describing two different
	// streams as if they were one.
	Generation uint64

	HasPAT        bool
	HasPMT        bool
	PMTVersion    uint8
	ProgramNumber uint16

	VideoPID    uint16
	VideoCodec  VideoCodec
	AudioPIDs   []uint16
	AudioTracks []AudioTrackInfo

	// ParameterSetsSeen reports whether the decoder configuration for the current
	// codec has been observed: SPS and PPS, plus VPS for HEVC.
	ParameterSetsSeen bool

	RandomAccess RandomAccessObservation
	Scrambling   StreamScrambling

	// CleanEntryPoints counts entry points whose own access unit contained no
	// scrambled packet. An entry point is only usable if this is non-zero.
	CleanEntryPoints uint64

	// CleanAccessUnits counts every complete picture that arrived without an
	// encrypted packet in it, joinable or not. This is what proves the receiver is
	// descrambling, and it becomes true up to a GOP before the next clean entry
	// point does.
	CleanAccessUnits uint64

	// AttachAvailable reports whether an entry point is currently within the buffer.
	AttachAvailable bool
}

// ReadinessFacts captures what the ring currently knows about this stream.
func (r *MasterRing) ReadinessFacts() ReadinessFacts {
	r.mu.Lock()
	defer r.mu.Unlock()

	pids := make([]uint16, len(r.audioPIDs))
	copy(pids, r.audioPIDs)

	tracks := make([]AudioTrackInfo, len(r.audioTracks))
	copy(tracks, r.audioTracks)

	parameterSets := false
	switch r.videoCodec {
	case CodecMPEG2:
		parameterSets = r.pesHasSPS // MPEG-2 Sequence Header (0xB3)
	case CodecH265:
		parameterSets = r.pesHasSPS && r.pesHasPPS && r.pesHasVPS
	default:
		parameterSets = r.pesHasSPS && r.pesHasPPS
	}

	attach := false
	if len(r.keyframeOffsets) > 0 {
		attach = r.keyframeOffsets[len(r.keyframeOffsets)-1] >= r.tail
	}

	return ReadinessFacts{
		Generation:        r.generation,
		HasPAT:            r.hasPATVersion,
		HasPMT:            r.hasPMTVersion,
		PMTVersion:        r.pmtVersion,
		ProgramNumber:     r.pmtProgramNumber,
		VideoPID:          r.videoPID,
		VideoCodec:        r.videoCodec,
		AudioPIDs:         pids,
		AudioTracks:       tracks,
		ParameterSetsSeen: parameterSets,
		RandomAccess:      r.raObs,
		Scrambling: StreamScrambling{
			VideoScrambled: r.scrambledVideoPackets,
			VideoClear:     r.clearVideoPackets,
			AudioScrambled: r.scrambledAudioPackets,
			AudioClear:     r.clearAudioPackets,
			VideoClearRun:  r.videoClearRun,
			AudioClearRun:  r.audioClearRun,
			AudioPIDs:      pids,
		},
		CleanEntryPoints: r.cleanRAPCount,
		CleanAccessUnits: r.cleanAccessUnits,
		AttachAvailable:  attach,
	}
}
