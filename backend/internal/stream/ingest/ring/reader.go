// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"errors"
	"io"
)

// SubscriberReader provides an independent read cursor over a MasterRing buffer.
// Synchronization is governed exclusively by the MasterRing mutex, completely eliminating deadlocks.
type SubscriberReader struct {
	ring         *MasterRing
	readOffset   int64
	droppedBytes int64
	isClosed     bool
}

var ErrNoKeyframeAvailable = errors.New("no valid keyframe available in ring")

// NewSubscriberReader creates a new subscriber reader at the specified absolute offset.
// If startOffset is negative or before ring.tail, it defaults to ring.tail.
func (r *MasterRing) NewSubscriberReader(startOffset int64) *SubscriberReader {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.newSubscriberReaderLocked(startOffset)
}

func (r *MasterRing) newSubscriberReaderLocked(startOffset int64) *SubscriberReader {
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

// NewPrimedSubscriber atomically captures the active PAT/PMT preamble and initializes
// a SubscriberReader positioned at the latest valid keyframe offset under a single lock.
func (r *MasterRing) NewPrimedSubscriber() (PrimedAttachPoint, *SubscriberReader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isClosed {
		return PrimedAttachPoint{}, nil, ErrRingClosed
	}

	if len(r.keyframeOffsets) == 0 {
		// Distinguish "no keyframe yet" (retry, the GOP boundary is still ahead) from
		// "no keyframe ever" (encrypted payload, retrying can only burn the timeout).
		if r.scrambledVideoConfirmedLocked() {
			return PrimedAttachPoint{}, nil, ErrScrambledStream
		}
		return PrimedAttachPoint{}, nil, ErrNoKeyframeAvailable
	}

	latestKf := r.keyframeOffsets[len(r.keyframeOffsets)-1]
	if latestKf < r.tail {
		return PrimedAttachPoint{}, nil, ErrNoKeyframeAvailable
	}

	var preamble []byte
	for _, pkt := range r.rawPATPackets {
		preamble = append(preamble, pkt...)
	}
	for _, pkt := range r.rawPMTPackets {
		preamble = append(preamble, pkt...)
	}

	attach := PrimedAttachPoint{
		Preamble:       preamble,
		KeyframeOffset: latestKf,
		Generation:     r.generation,
		HasKeyframe:    true,
	}

	reader := r.newSubscriberReaderLocked(latestKf)
	return attach, reader, nil
}

// Read reads up to len(p) bytes from the master ring buffer.
// It blocks until data is available or the master ring is closed.
func (s *SubscriberReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	maxRead := len(p)
	if maxRead >= TSPacketSize {
		maxRead = (maxRead / TSPacketSize) * TSPacketSize
	}

	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()

	for {
		if s.isClosed {
			return 0, io.EOF
		}

		if s.ring.isClosed && s.readOffset >= s.ring.head {
			return 0, io.EOF
		}

		// Check if slow subscriber was overtaken by ring write head
		if s.readOffset < s.ring.tail {
			s.droppedBytes += (s.ring.tail - s.readOffset)
			s.readOffset = s.ring.tail
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
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()

	if s.isClosed {
		return 0, io.EOF
	}

	if len(s.ring.keyframeOffsets) == 0 {
		return 0, ErrNoKeyframeFound
	}
	latest := s.ring.keyframeOffsets[len(s.ring.keyframeOffsets)-1]
	if latest < s.ring.tail {
		return 0, ErrNoKeyframeFound
	}

	s.readOffset = latest
	return latest, nil
}

// DroppedBytes returns the number of bytes dropped due to ring buffer overrun.
func (s *SubscriberReader) DroppedBytes() int64 {
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()
	return s.droppedBytes
}

// Offset returns the current absolute read position.
func (s *SubscriberReader) Offset() int64 {
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()
	return s.readOffset
}

// Close closes the subscriber reader and wakes any waiting operations.
func (s *SubscriberReader) Close() error {
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()

	s.isClosed = true
	s.ring.notEmpty.Broadcast()
	return nil
}
