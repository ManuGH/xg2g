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

// MasterRing is a thread-safe, multi-reader circular FIFO buffer for MPEG-TS streams.
// It maintains an in-band index of PAT, PMT, and Keyframe byte offsets for instant subscriber attachment.
type MasterRing struct {
	mu       sync.Mutex
	notEmpty *sync.Cond
	buf      []byte
	capacity int
	head     int64 // total bytes written monotonically
	tail     int64 // oldest valid byte offset in buffer
	isClosed bool

	// In-band indexing
	latestPAT       []byte
	latestPMT       []byte
	pmtPID          uint16
	keyframeOffsets []int64 // circular list of absolute head offsets
	maxKeyframes    int
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
		maxKeyframes: 32,
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

	// Index each 188-byte packet in the chunk
	for i := 0; i < n; i += TSPacketSize {
		pkt := data[i : i+TSPacketSize]
		pktOffset := r.head + int64(i)
		r.indexPacketLocked(pkt, pktOffset)
	}

	// Write data into circular buffer
	writePos := int(r.head % int64(r.capacity))
	firstChunk := r.capacity - writePos
	if n <= firstChunk {
		copy(r.buf[writePos:], data)
	} else {
		copy(r.buf[writePos:], data[:firstChunk])
		copy(r.buf[:n-firstChunk], data[firstChunk:])
	}

	r.head += int64(n)
	if r.head-r.tail > int64(r.capacity) {
		r.tail = r.head - int64(r.capacity)
		r.pruneKeyframesLocked()
	}

	r.notEmpty.Broadcast()
	return n, nil
}

func (r *MasterRing) indexPacketLocked(pkt []byte, offset int64) {
	if len(pkt) < TSPacketSize || pkt[0] != SyncByte {
		return
	}

	pid := (uint16(pkt[1]&0x1F) << 8) | uint16(pkt[2])
	afc := (pkt[3] >> 4) & 0x03
	hasAF := (afc == 0x02 || afc == 0x03)
	hasPayload := (afc == 0x01 || afc == 0x03)

	// Index PAT (PID 0)
	if pid == 0 && hasPayload {
		if r.latestPAT == nil {
			r.latestPAT = make([]byte, TSPacketSize)
		}
		copy(r.latestPAT, pkt)
		r.extractPMTPIDLocked(pkt)
		return
	}

	// Index PMT
	if r.pmtPID > 0 && pid == r.pmtPID && hasPayload {
		if r.latestPMT == nil {
			r.latestPMT = make([]byte, TSPacketSize)
		}
		copy(r.latestPMT, pkt)
		return
	}

	// Index Keyframe / IDR Access Point
	isKeyframe := false
	if hasAF && len(pkt) >= 6 {
		afLen := int(pkt[4])
		if afLen > 0 {
			flags := pkt[5]
			// random_access_indicator bit in adaptation field
			if (flags & 0x40) != 0 {
				isKeyframe = true
			}
		}
	}

	// Fallback: scan payload for H.264 NAL type 5 (IDR) or 7 (SPS)
	if !isKeyframe && hasPayload {
		payloadStart := 4
		if hasAF {
			payloadStart = 5 + int(pkt[4])
		}
		if payloadStart < TSPacketSize-4 {
			p := pkt[payloadStart:]
			for j := 0; j < len(p)-4; j++ {
				if p[j] == 0x00 && p[j+1] == 0x00 && p[j+2] == 0x01 {
					nalType := p[j+3] & 0x1F
					if nalType == 5 || nalType == 7 { // IDR or SPS
						isKeyframe = true
						break
					}
				}
			}
		}
	}

	if isKeyframe {
		r.keyframeOffsets = append(r.keyframeOffsets, offset)
		if len(r.keyframeOffsets) > r.maxKeyframes {
			r.keyframeOffsets = r.keyframeOffsets[1:]
		}
	}
}

func (r *MasterRing) extractPMTPIDLocked(patPkt []byte) {
	// Simple PAT parser: find program number > 0 and extract PMT PID
	// Skip TS header (4 bytes) + optional pointer field
	if len(patPkt) < 16 {
		return
	}
	payload := patPkt[4:]
	pointerField := int(payload[0])
	if 1+pointerField >= len(payload) {
		return
	}
	table := payload[1+pointerField:]
	if len(table) < 8 || table[0] != 0x00 { // table_id must be 0x00 for PAT
		return
	}
	sectionLen := int((uint16(table[1]&0x0F) << 8) | uint16(table[2]))
	if sectionLen < 9 || sectionLen+3 > len(table) {
		return
	}

	// Program loop starts at offset 8, each entry is 4 bytes (program_number uint16, PID uint16)
	programs := table[8 : sectionLen-1] // exclude CRC32
	for i := 0; i+4 <= len(programs); i += 4 {
		progNum := (uint16(programs[i]) << 8) | uint16(programs[i+1])
		progPID := ((uint16(programs[i+2]) & 0x1F) << 8) | uint16(programs[i+3])
		if progNum > 0 { // Skip network PID (prog 0)
			r.pmtPID = progPID
			break
		}
	}
}

func (r *MasterRing) pruneKeyframesLocked() {
	validIdx := 0
	for i, offset := range r.keyframeOffsets {
		if offset >= r.tail {
			validIdx = i
			break
		}
	}
	if validIdx > 0 {
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

// PATPMTPreamble returns concatenated latest PAT and PMT packets if available.
func (r *MasterRing) PATPMTPreamble() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	var preamble []byte
	if len(r.latestPAT) == TSPacketSize {
		preamble = append(preamble, r.latestPAT...)
	}
	if len(r.latestPMT) == TSPacketSize {
		preamble = append(preamble, r.latestPMT...)
	}
	return preamble
}

// Head returns the current total bytes written.
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
