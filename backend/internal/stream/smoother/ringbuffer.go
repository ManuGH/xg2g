// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"errors"
	"sync"
)

var (
	ErrBufferOverflow = errors.New("ring buffer overflow: capacity exceeded")
	ErrBufferClosed   = errors.New("ring buffer closed")
)

// TSRingBuffer is a thread-safe, 188-byte aligned FIFO ring buffer.
type TSRingBuffer struct {
	mu        sync.Mutex
	notEmpty  *sync.Cond
	notFull   *sync.Cond
	buf       []byte
	head      int // read position
	tail      int // write position
	count     int // valid bytes in buffer
	capacity  int
	isClosed  bool
	underruns int64
	overflows int64
}

// NewTSRingBuffer creates a new TS ring buffer with the given capacity.
func NewTSRingBuffer(capacityBytes int) *TSRingBuffer {
	// Align capacity to 188 bytes
	capacityBytes = (capacityBytes / TSPacketSize) * TSPacketSize
	if capacityBytes < TSPacketSize*100 {
		capacityBytes = TSPacketSize * 1000 // min ~188 KiB
	}

	rb := &TSRingBuffer{
		buf:      make([]byte, capacityBytes),
		capacity: capacityBytes,
	}
	rb.notEmpty = sync.NewCond(&rb.mu)
	rb.notFull = sync.NewCond(&rb.mu)
	return rb
}

// Push writes a chunk of TS packets into the ring buffer.
func (rb *TSRingBuffer) Push(data []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.isClosed {
		return 0, ErrBufferClosed
	}

	n := len(data)
	if n == 0 {
		return 0, nil
	}

	// If data exceeds available space:
	available := rb.capacity - rb.count
	if n > available {
		rb.overflows++
		return 0, ErrBufferOverflow
	}

	// Copy data in 1 or 2 chunks (wrapping around tail)
	if rb.tail+n <= rb.capacity {
		copy(rb.buf[rb.tail:rb.tail+n], data)
		rb.tail = (rb.tail + n) % rb.capacity
	} else {
		firstChunk := rb.capacity - rb.tail
		copy(rb.buf[rb.tail:], data[:firstChunk])
		secondChunk := n - firstChunk
		copy(rb.buf[:secondChunk], data[firstChunk:])
		rb.tail = secondChunk
	}

	rb.count += n
	rb.notEmpty.Signal()
	return n, nil
}

// Pop extracts up to maxBytes (aligned to 188 bytes) from the ring buffer.
// If nonBlocking is true and no data is available, returns nil immediately.
func (rb *TSRingBuffer) Pop(maxBytes int) ([]byte, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		if rb.isClosed {
			return nil, false
		}
		rb.underruns++
		return nil, true
	}

	// Align to 188 bytes
	toRead := (maxBytes / TSPacketSize) * TSPacketSize
	if toRead == 0 {
		toRead = TSPacketSize
	}
	if toRead > rb.count {
		toRead = (rb.count / TSPacketSize) * TSPacketSize
	}
	if toRead == 0 {
		return nil, true
	}

	out := make([]byte, toRead)
	if rb.head+toRead <= rb.capacity {
		copy(out, rb.buf[rb.head:rb.head+toRead])
		rb.head = (rb.head + toRead) % rb.capacity
	} else {
		firstChunk := rb.capacity - rb.head
		copy(out[:firstChunk], rb.buf[rb.head:])
		secondChunk := toRead - firstChunk
		copy(out[firstChunk:], rb.buf[:secondChunk])
		rb.head = secondChunk
	}

	rb.count -= toRead
	rb.notFull.Signal()
	return out, true
}

// BufferedBytes returns the number of unconsumed bytes currently in the buffer.
func (rb *TSRingBuffer) BufferedBytes() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// BufferedMediaMs converts buffered bytes to milliseconds of playback based on bitrate.
func (rb *TSRingBuffer) BufferedMediaMs(bitrate float64) float64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if bitrate <= 0 {
		bitrate = 4_500_000.0
	}
	return (float64(rb.count*8) / bitrate) * 1000.0
}

// Stats returns underrun and overflow counters.
func (rb *TSRingBuffer) Stats() (underruns int64, overflows int64) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.underruns, rb.overflows
}

// Close closes the ring buffer, waking up any waiting readers.
func (rb *TSRingBuffer) Close() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.isClosed = true
	rb.notEmpty.Broadcast()
	rb.notFull.Broadcast()
}
