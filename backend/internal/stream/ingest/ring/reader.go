// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"io"
	"sync"
)

// SubscriberReader provides an independent read cursor over a MasterRing buffer.
type SubscriberReader struct {
	mu           sync.Mutex
	ring         *MasterRing
	readOffset   int64
	droppedBytes int64
	isClosed     bool
}

// NewSubscriberReader creates a new subscriber reader at the specified absolute offset.
// If startOffset is negative or before ring.tail, it defaults to ring.tail.
func (r *MasterRing) NewSubscriberReader(startOffset int64) *SubscriberReader {
	r.mu.Lock()
	defer r.mu.Unlock()

	if startOffset < r.tail {
		startOffset = r.tail
	}
	if startOffset > r.head {
		startOffset = r.head
	}

	return &SubscriberReader{
		ring:       r,
		readOffset: startOffset,
	}
}

// Read reads up to len(p) bytes from the master ring buffer.
// It blocks until data is available or the master ring is closed.
func (s *SubscriberReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Read in 188-byte packet multiples if possible
	maxRead := len(p)
	if maxRead >= TSPacketSize {
		maxRead = (maxRead / TSPacketSize) * TSPacketSize
	}

	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()

	for {
		s.mu.Lock()
		if s.isClosed {
			s.mu.Unlock()
			return 0, io.EOF
		}
		s.mu.Unlock()

		if s.ring.isClosed && s.readOffset >= s.ring.head {
			return 0, io.EOF
		}

		// Check if slow subscriber was overtaken by ring write head
		if s.readOffset < s.ring.tail {
			s.mu.Lock()
			s.droppedBytes += (s.ring.tail - s.readOffset)
			s.readOffset = s.ring.tail
			s.mu.Unlock()
		}

		available := int(s.ring.head - s.readOffset)
		if available > 0 {
			toRead := available
			if toRead > maxRead {
				toRead = maxRead
			}

			readPos := int(s.readOffset % int64(s.ring.capacity))
			firstChunk := s.ring.capacity - readPos
			if toRead <= firstChunk {
				copy(p[:toRead], s.ring.buf[readPos:readPos+toRead])
			} else {
				copy(p[:firstChunk], s.ring.buf[readPos:])
				copy(p[firstChunk:toRead], s.ring.buf[:toRead-firstChunk])
			}

			s.readOffset += int64(toRead)
			return toRead, nil
		}

		// No data available yet, wait for push
		s.ring.notEmpty.Wait()
	}
}

// SeekToLatestKeyframe moves the subscriber read cursor to the latest decodable keyframe.
func (s *SubscriberReader) SeekToLatestKeyframe() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return 0, io.EOF
	}

	offset, ok := s.ring.LatestKeyframeOffset()
	if !ok {
		return 0, ErrNoKeyframeFound
	}

	s.readOffset = offset
	return offset, nil
}

// DroppedBytes returns the number of bytes dropped due to ring buffer overrun.
func (s *SubscriberReader) DroppedBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.droppedBytes
}

// Offset returns the current absolute read position.
func (s *SubscriberReader) Offset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readOffset
}

// Close closes the subscriber reader.
func (s *SubscriberReader) Close() error {
	s.mu.Lock()
	s.isClosed = true
	s.mu.Unlock()

	// Signal ring to wake in case of waiting
	s.ring.mu.Lock()
	s.ring.notEmpty.Broadcast()
	s.ring.mu.Unlock()
	return nil
}
