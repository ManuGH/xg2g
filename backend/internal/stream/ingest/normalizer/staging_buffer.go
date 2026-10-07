// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package normalizer

import (
	"sync"
	"time"
)

// StagingBuffer is a bounded, thread-safe circular FIFO buffer for MPEG-TS packets.
// If the buffer fills because egress is stalled, it fails closed with ErrStagingBufferOverflow.
type StagingBuffer struct {
	mu       sync.Mutex
	notEmpty *sync.Cond
	buf      []byte
	capacity int
	head     int // write index
	tail     int // read index
	count    int // valid unread bytes
	isClosed bool
	spaceCh  chan struct{}
}

// NewStagingBuffer creates a StagingBuffer with the specified byte capacity aligned to 188 bytes.
func NewStagingBuffer(capacityBytes int) *StagingBuffer {
	capacityBytes = (capacityBytes / TSPacketSize) * TSPacketSize
	if capacityBytes < TSPacketSize*10 {
		capacityBytes = TSPacketSize * 10
	}

	sb := &StagingBuffer{
		buf:      make([]byte, capacityBytes),
		capacity: capacityBytes,
		spaceCh:  make(chan struct{}, 1),
	}
	sb.notEmpty = sync.NewCond(&sb.mu)
	return sb
}

const defaultStallTimeout = 5 * time.Second

// Write writes data into the staging buffer. If the buffer is full, it waits up to 5s
// for egress to drain space before declaring a stalled sink with ErrStagingBufferOverflow.
func (sb *StagingBuffer) Write(p []byte) (int, error) {
	return sb.WriteWithTimeout(p, defaultStallTimeout)
}

// WriteWithTimeout writes data into the staging buffer, waiting up to timeout for available space.
func (sb *StagingBuffer) WriteWithTimeout(p []byte, timeout time.Duration) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.isClosed {
		return 0, ErrNormalizerClosed
	}

	if len(p) > sb.capacity {
		return 0, ErrStagingBufferOverflow
	}

	if sb.count+len(p) > sb.capacity {
		deadline := time.Now().Add(timeout)
		for sb.count+len(p) > sb.capacity {
			if sb.isClosed {
				return 0, ErrNormalizerClosed
			}
			rem := time.Until(deadline)
			if rem <= 0 {
				return 0, ErrStagingBufferOverflow
			}

			spaceCh := sb.spaceCh
			sb.mu.Unlock()

			timer := time.NewTimer(rem)
			select {
			case <-spaceCh:
				timer.Stop()
			case <-timer.C:
			}

			sb.mu.Lock()
		}
	}

	n := len(p)
	firstChunk := sb.capacity - sb.head
	if n <= firstChunk {
		copy(sb.buf[sb.head:], p)
	} else {
		copy(sb.buf[sb.head:], p[:firstChunk])
		copy(sb.buf[:n-firstChunk], p[firstChunk:])
	}

	sb.head = (sb.head + n) % sb.capacity
	sb.count += n

	sb.notEmpty.Broadcast()
	return n, nil
}

// Read reads up to len(p) available bytes without blocking. Returns number of bytes read.
func (sb *StagingBuffer) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.count == 0 {
		if sb.isClosed {
			return 0, ErrNormalizerClosed
		}
		return 0, nil
	}

	n := len(p)
	if n > sb.count {
		n = sb.count
	}

	firstChunk := sb.capacity - sb.tail
	if n <= firstChunk {
		copy(p[:n], sb.buf[sb.tail:sb.tail+n])
	} else {
		copy(p[:firstChunk], sb.buf[sb.tail:])
		copy(p[firstChunk:n], sb.buf[:n-firstChunk])
	}

	sb.tail = (sb.tail + n) % sb.capacity
	sb.count -= n

	select {
	case sb.spaceCh <- struct{}{}:
	default:
	}

	return n, nil
}

// ReadBlocking waits until at least minBytes are available or the buffer is closed.
func (sb *StagingBuffer) ReadBlocking(p []byte, minBytes int) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if minBytes > len(p) {
		minBytes = len(p)
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()

	for sb.count < minBytes && !sb.isClosed {
		sb.notEmpty.Wait()
	}

	if sb.count == 0 {
		if sb.isClosed {
			return 0, ErrNormalizerClosed
		}
		return 0, nil
	}

	n := len(p)
	if n > sb.count {
		n = sb.count
	}

	firstChunk := sb.capacity - sb.tail
	if n <= firstChunk {
		copy(p[:n], sb.buf[sb.tail:sb.tail+n])
	} else {
		copy(p[:firstChunk], sb.buf[sb.tail:])
		copy(p[firstChunk:n], sb.buf[:n-firstChunk])
	}

	sb.tail = (sb.tail + n) % sb.capacity
	sb.count -= n

	select {
	case sb.spaceCh <- struct{}{}:
	default:
	}

	return n, nil
}

// BufferedBytes returns the total unread bytes in the buffer.
func (sb *StagingBuffer) BufferedBytes() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.count
}

// Capacity returns the total byte capacity of the buffer.
func (sb *StagingBuffer) Capacity() int {
	return sb.capacity
}

// Close closes the buffer, waking any waiting readers or writers.
func (sb *StagingBuffer) Close() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.isClosed = true
	sb.notEmpty.Broadcast()
	select {
	case sb.spaceCh <- struct{}{}:
	default:
	}
}

// Reset clears the buffer contents.
func (sb *StagingBuffer) Reset() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.head = 0
	sb.tail = 0
	sb.count = 0
	sb.isClosed = false
	select {
	case sb.spaceCh <- struct{}{}:
	default:
	}
}
