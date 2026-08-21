// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
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

type sectionAssembler struct {
	buf        []byte
	sectionLen int
	lastCC     uint8
	hasCC      bool
	rawPackets [][]byte
}

func (s *sectionAssembler) reset() {
	s.buf = s.buf[:0]
	s.sectionLen = 0
	s.hasCC = false
	s.rawPackets = s.rawPackets[:0]
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

	// PSI Table Assembly
	patAssembler  sectionAssembler
	pmtAssembler  sectionAssembler
	rawPATPackets [][]byte
	rawPMTPackets [][]byte
	patVersion    uint8
	pmtVersion    uint8
	pmtPID        uint16
	videoPID      uint16
	videoCodec    VideoCodec

	// Stateful Video Parsing & Keyframe Indexing
	currentPESOffset int64
	pesHasKeyframe   bool
	annexBState      uint32 // 4-byte shift register for startcode scanning
	expectingNALByte bool
	keyframeOffsets  []int64
	maxKeyframes     int
}

// NewMasterRing creates a new MasterRing with the specified capacity (aligned to 188 bytes).
func NewMasterRing(capacityBytes int) *MasterRing {
	capacityBytes = (capacityBytes / TSPacketSize) * TSPacketSize
	if capacityBytes < TSPacketSize*5 {
		capacityBytes = TSPacketSize * 5 // min 5 packets (~940 bytes)
	}

	r := &MasterRing{
		buf:          make([]byte, capacityBytes),
		capacity:     capacityBytes,
		videoCodec:   CodecUnknown,
		maxKeyframes: 64,
		annexBState:  0xFFFFFFFF,
	}
	r.notEmpty = sync.NewCond(&r.mu)
	return r
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
		r.assemblePATSectionLocked(pkt, pusi, payload)
		return
	}

	// 2. PMT Section Assembly
	if r.pmtPID > 0 && pid == r.pmtPID {
		r.assemblePMTSectionLocked(pkt, pusi, payload)
		return
	}

	// 3. Video Elementary Stream Keyframe / IDR Indexing
	if r.videoPID > 0 && pid == r.videoPID {
		r.parseVideoPacketLocked(pkt, offset, pusi, payload)
	}
}

func (r *MasterRing) assemblePATSectionLocked(pkt []byte, pusi bool, payload []byte) {
	cc := pkt[3] & 0x0F
	if pusi {
		r.patAssembler.reset()
		r.patAssembler.lastCC = cc
		r.patAssembler.hasCC = true
		if len(payload) < 1 {
			return
		}
		pointer := int(payload[0])
		if 1+pointer >= len(payload) {
			return
		}
		table := payload[1+pointer:]
		if len(table) < 3 || table[0] != 0x00 { // table_id must be 0 for PAT
			return
		}
		secLen := int((uint16(table[1]&0x0F) << 8) | uint16(table[2]))
		r.patAssembler.sectionLen = secLen + 3
		toAppend := len(table)
		if toAppend > r.patAssembler.sectionLen {
			toAppend = r.patAssembler.sectionLen
		}
		r.patAssembler.buf = append(r.patAssembler.buf, table[:toAppend]...)
		r.patAssembler.rawPackets = append(r.patAssembler.rawPackets, cloneSlice(pkt))
	} else if r.patAssembler.sectionLen > 0 && len(r.patAssembler.buf) < r.patAssembler.sectionLen {
		if r.patAssembler.hasCC && cc != (r.patAssembler.lastCC+1)&0x0F {
			// CC discontinuity detected: abort corrupted in-flight assembly
			r.patAssembler.reset()
			return
		}
		r.patAssembler.lastCC = cc
		needed := r.patAssembler.sectionLen - len(r.patAssembler.buf)
		toAppend := len(payload)
		if toAppend > needed {
			toAppend = needed
		}
		r.patAssembler.buf = append(r.patAssembler.buf, payload[:toAppend]...)
		r.patAssembler.rawPackets = append(r.patAssembler.rawPackets, cloneSlice(pkt))
	}

	if r.patAssembler.sectionLen > 0 && len(r.patAssembler.buf) >= r.patAssembler.sectionLen {
		// PAT complete: verify current_next and parse PMT PID
		table := r.patAssembler.buf
		if len(table) >= 12 {
			currentNext := table[5] & 0x01
			if currentNext == 1 { // Only process active table
				version := (table[5] >> 1) & 0x1F
				r.patVersion = version

				programs := table[8 : r.patAssembler.sectionLen-4] // exclude CRC32
				for i := 0; i+4 <= len(programs); i += 4 {
					progNum := (uint16(programs[i]) << 8) | uint16(programs[i+1])
					progPID := ((uint16(programs[i+2]) & 0x1F) << 8) | uint16(programs[i+3])
					if progNum > 0 {
						if r.pmtPID != progPID {
							r.pmtPID = progPID
							r.pmtAssembler.reset()
							r.videoPID = 0
							r.videoCodec = CodecUnknown
							r.rawPMTPackets = nil
						}
						break
					}
				}
				r.rawPATPackets = cloneSliceList(r.patAssembler.rawPackets)
			}
		}
	}
}

func (r *MasterRing) assemblePMTSectionLocked(pkt []byte, pusi bool, payload []byte) {
	cc := pkt[3] & 0x0F
	if pusi {
		r.pmtAssembler.reset()
		r.pmtAssembler.lastCC = cc
		r.pmtAssembler.hasCC = true
		if len(payload) < 1 {
			return
		}
		pointer := int(payload[0])
		if 1+pointer >= len(payload) {
			return
		}
		table := payload[1+pointer:]
		if len(table) < 3 || table[0] != 0x02 { // table_id must be 2 for PMT
			return
		}
		secLen := int((uint16(table[1]&0x0F) << 8) | uint16(table[2]))
		r.pmtAssembler.sectionLen = secLen + 3
		toAppend := len(table)
		if toAppend > r.pmtAssembler.sectionLen {
			toAppend = r.pmtAssembler.sectionLen
		}
		r.pmtAssembler.buf = append(r.pmtAssembler.buf, table[:toAppend]...)
		r.pmtAssembler.rawPackets = append(r.pmtAssembler.rawPackets, cloneSlice(pkt))
	} else if r.pmtAssembler.sectionLen > 0 && len(r.pmtAssembler.buf) < r.pmtAssembler.sectionLen {
		if r.pmtAssembler.hasCC && cc != (r.pmtAssembler.lastCC+1)&0x0F {
			// CC discontinuity detected: abort corrupted in-flight assembly
			r.pmtAssembler.reset()
			return
		}
		r.pmtAssembler.lastCC = cc
		needed := r.pmtAssembler.sectionLen - len(r.pmtAssembler.buf)
		toAppend := len(payload)
		if toAppend > needed {
			toAppend = needed
		}
		r.pmtAssembler.buf = append(r.pmtAssembler.buf, payload[:toAppend]...)
		r.pmtAssembler.rawPackets = append(r.pmtAssembler.rawPackets, cloneSlice(pkt))
	}

	if r.pmtAssembler.sectionLen > 0 && len(r.pmtAssembler.buf) >= r.pmtAssembler.sectionLen {
		// PMT complete: verify current_next and parse video stream type and elementary PID
		table := r.pmtAssembler.buf
		if len(table) >= 12 {
			currentNext := table[5] & 0x01
			if currentNext == 1 { // Only process active table
				version := (table[5] >> 1) & 0x1F
				r.pmtVersion = version

				progInfoLen := int((uint16(table[10]&0x0F) << 8) | uint16(table[11]))
				esStart := 12 + progInfoLen
				esEnd := r.pmtAssembler.sectionLen - 4 // exclude CRC32

				for i := esStart; i+5 <= esEnd && i < len(table); {
					st := table[i]
					elemPID := ((uint16(table[i+1]) & 0x1F) << 8) | uint16(table[i+2])
					esInfoLen := int((uint16(table[i+3]&0x0F) << 8) | uint16(table[i+4]))

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
				r.rawPMTPackets = cloneSliceList(r.pmtAssembler.rawPackets)
			}
		}
	}
}

func (r *MasterRing) parseVideoPacketLocked(pkt []byte, offset int64, pusi bool, payload []byte) {
	esData := payload

	if pusi {
		// Video PES packet start: verify PES startcode prefix (00 00 01 E0..EF)
		if len(payload) >= 9 && payload[0] == 0x00 && payload[1] == 0x00 && payload[2] == 0x01 && (payload[3] >= 0xE0 && payload[3] <= 0xEF) {
			r.currentPESOffset = offset
			r.pesHasKeyframe = false
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
			isIDR := false

			if r.videoCodec == CodecH264 {
				nalType := b & 0x1F
				if nalType == 5 { // IDR Slice
					isIDR = true
				}
			} else if r.videoCodec == CodecH265 {
				nalType := (b >> 1) & 0x3F
				if nalType == 19 || nalType == 20 || nalType == 21 { // IDR_W_RADL, IDR_N_LP, CRA_NUT
					isIDR = true
				}
			}

			if isIDR && !r.pesHasKeyframe && r.currentPESOffset >= 0 {
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

// PATPMTPreamble returns concatenated raw PAT and PMT TS packets.
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
