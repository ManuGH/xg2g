// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

// packetStore is a generic, pure circular byte buffer for monotonic stream storage.
// It possesses zero media semantics: it has no knowledge of transport streams,
// 188-byte packet sizing, PSI, codecs, RAPs, or presentation time.
//
// Synchronization and lifecycle are owned exclusively by MasterRing (mu).
// As such, packetStore carries no internal lock and no isClosed state.
type packetStore struct {
	buf      []byte
	capacity int
	head     int64 // total bytes written monotonically
	tail     int64 // oldest valid byte offset in buffer
}

// newPacketStore creates a new pure byte store with the given capacity.
// Capacity validation and transport alignment are performed by the caller.
func newPacketStore(capacityBytes int) *packetStore {
	if capacityBytes < 1 {
		capacityBytes = 1
	}
	return &packetStore{
		buf:      make([]byte, capacityBytes),
		capacity: capacityBytes,
	}
}

// writeCommitted writes bytes into the circular buffer and advances monotonic head and tail.
// This is an infallible internal operation: once commit validations have passed,
// the memory copy cannot fail. Chunks larger than capacity are supported deterministically
// without panic.
func (ps *packetStore) writeCommitted(data []byte) (head int64, tail int64) {
	remaining := data
	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > ps.capacity {
			chunk = chunk[:ps.capacity]
		}
		remaining = remaining[len(chunk):]

		chunkLen := len(chunk)
		writePos := int(ps.head % int64(ps.capacity))
		firstChunk := ps.capacity - writePos
		if chunkLen <= firstChunk {
			copy(ps.buf[writePos:], chunk)
		} else {
			copy(ps.buf[writePos:], chunk[:firstChunk])
			copy(ps.buf[:chunkLen-firstChunk], chunk[firstChunk:])
		}

		ps.head += int64(chunkLen)
		if ps.head-ps.tail > int64(ps.capacity) {
			ps.tail = ps.head - int64(ps.capacity)
		}
	}
	return ps.head, ps.tail
}

// readAt reads up to len(p) bytes starting from the absolute monotonic offset.
//
// If offset < tail, the requested bytes were already evicted and ErrSubscriberOverrun is returned.
// If offset >= head, zero bytes are read and no error is returned (caller should wait for data).
// Otherwise, it copies min(len(p), head - offset) bytes and returns the number of bytes read
// alongside the next monotonic offset.
func (ps *packetStore) readAt(p []byte, offset int64) (int, int64, error) {
	if offset < ps.tail {
		return 0, offset, ErrSubscriberOverrun
	}
	available := int(ps.head - offset)
	if available <= 0 || len(p) == 0 {
		return 0, offset, nil
	}

	toRead := len(p)
	if toRead > available {
		toRead = available
	}

	readPos := int(offset % int64(ps.capacity))
	firstChunk := ps.capacity - readPos
	if toRead <= firstChunk {
		copy(p[:toRead], ps.buf[readPos:readPos+toRead])
	} else {
		copy(p[:firstChunk], ps.buf[readPos:])
		copy(p[firstChunk:toRead], ps.buf[:toRead-firstChunk])
	}

	return toRead, offset + int64(toRead), nil
}

func (ps *packetStore) headOffset() int64 {
	return ps.head
}

func (ps *packetStore) tailOffset() int64 {
	return ps.tail
}

func (ps *packetStore) capacityBytes() int {
	return ps.capacity
}

func (ps *packetStore) bufferedBytes() int {
	return int(ps.head - ps.tail)
}
