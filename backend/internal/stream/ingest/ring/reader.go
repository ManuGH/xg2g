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
	ring       *MasterRing
	readOffset int64
	isClosed   bool

	// droppedBytes counts only what the ring discarded before this subscriber
	// read it. resyncSkippedBytes counts what recovery then chose to skip on top
	// of that, to reach a decodable boundary. Keeping them apart matters: the
	// first is pressure on the ring, the second is a deliberate correction, and
	// summing them would make every overrun look worse than it was.
	droppedBytes       int64
	resyncSkippedBytes int64

	// overruns counts how often the ring overtook this subscriber. It is the event
	// count behind droppedBytes: one long stall and many short ones can discard the
	// same number of bytes and are not the same fault.
	overruns int64

	// pendingPrefix holds PAT/PMT packets that must reach the consumer before any
	// further ring data, so a decoder re-entering after an overrun is told the
	// topology before it is handed pictures. pendingPrefixGeneration is the ring
	// generation it describes: delivery can span several reads, and a preamble that
	// outlives its generation must be dropped rather than finished.
	pendingPrefix           []byte
	pendingPrefixGeneration uint64

	// awaitingRandomAccess marks a subscriber that was overtaken and has not found
	// a decodable re-entry point yet. It stays set across wake-ups until one exists.
	awaitingRandomAccess bool
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

	latestKf, ok := r.latestKeyframeOffsetLocked()
	if !ok {
		return PrimedAttachPoint{}, nil, ErrNoKeyframeAvailable
	}

	attach := PrimedAttachPoint{
		Preamble:       r.patpmtPreambleLocked(),
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

		// A queued preamble is delivered ahead of any ring data, and it is never
		// interleaved with it. A caller with a small buffer receives it across
		// several reads; ring bytes only resume once the last of it is gone.
		//
		// Delivery spans several reads, so the preamble can go stale midway: a PMT
		// bump between two of them would otherwise let the tail of one generation's
		// topology reach a consumer the ring has already moved past. Atomic attach
		// is not only about how the pair is captured but about how it is handed
		// over, so a generation change discards what is left and starts recovery
		// again from the new one.
		if len(s.pendingPrefix) > 0 {
			if s.pendingPrefixGeneration != s.ring.generation {
				s.pendingPrefix = nil
				s.awaitingRandomAccess = true
				continue
			}

			n := copy(p[:maxRead], s.pendingPrefix)
			s.pendingPrefix = s.pendingPrefix[n:]
			if len(s.pendingPrefix) == 0 {
				s.pendingPrefix = nil
			}
			return n, nil
		}

		if s.ring.isClosed && s.readOffset >= s.ring.head {
			return 0, io.EOF
		}

		// Overtaken by the write head: the ring discarded bytes this subscriber had
		// not read yet. Resuming at the tail would put a decoder at whatever offset
		// the eviction happened to leave behind, which is mid-GOP in all but the
		// luckiest case. Recovery re-enters at a random access point instead, with
		// the active topology restated in front of it.
		if s.readOffset < s.ring.tail {
			s.droppedBytes += s.ring.tail - s.readOffset
			s.overruns++
			s.readOffset = s.ring.tail

			// Whether the tail is an acceptable place to resume has three answers,
			// and collapsing the first two is how a video service gets a mid-GOP
			// entry during startup:
			//
			//	topology unknown       - no complete PMT parsed yet. Nothing is known
			//	                         about this stream, so nothing licenses the
			//	                         tail. Wait; a service that turns out to
			//	                         carry video must not already have been given
			//	                         bytes from the middle of a picture.
			//	topology known, audio  - the PMT names no decodable video. There are
			//	                         no random access points to wait for and no
			//	                         decoder state to corrupt, so the tail is the
			//	                         only entry point there is, and a legal one.
			//	topology known, video  - only a random access point will do.
			s.awaitingRandomAccess = !s.ring.facts.HasPMT || s.ring.facts.VideoPID != 0
		}

		// The wait is a state, not a single pass. Without it the next push would
		// wake this reader with readOffset already equal to the tail, the overrun
		// branch above would not fire again, and the ring bytes at the tail - the
		// mid-GOP bytes this whole path exists to refuse - would be delivered.
		if s.awaitingRandomAccess {
			if s.resyncToRandomAccessLocked() {
				s.awaitingRandomAccess = false
				continue
			}
			if s.ring.isClosed {
				return 0, io.EOF
			}
			s.ring.notEmpty.Wait()
			continue
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

// resyncToRandomAccessLocked moves the read cursor to the newest random access point
// the ring still holds and queues the active PAT/PMT ahead of it. It reports false
// when there is no such point right now, which is a transient state rather than an
// error: the next GOP boundary, or the first one of a new generation, is still ahead.
//
// Bytes skipped between the tail and the chosen keyframe are accounted separately
// from the overrun itself. They were available; recovery chose not to deliver them.
func (s *SubscriberReader) resyncToRandomAccessLocked() bool {
	latest, ok := s.ring.latestKeyframeOffsetLocked()
	if !ok {
		return false
	}

	if latest > s.readOffset {
		s.resyncSkippedBytes += latest - s.readOffset
	}
	s.readOffset = latest
	s.pendingPrefix = s.ring.patpmtPreambleLocked()
	s.pendingPrefixGeneration = s.ring.generation
	return true
}

// SeekToLatestKeyframe moves the subscriber read cursor to the latest decodable keyframe.
func (s *SubscriberReader) SeekToLatestKeyframe() (int64, error) {
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()

	if s.isClosed {
		return 0, io.EOF
	}

	latest, ok := s.ring.latestKeyframeOffsetLocked()
	if !ok {
		return 0, ErrNoKeyframeFound
	}

	s.readOffset = latest
	return latest, nil
}

// SubscriberStats is one consistent snapshot of a subscriber's ring accounting,
// taken under a single ring lock. The fields only mean something together: LagBytes
// is what this subscriber has not read yet, and the two byte counters are what it
// will never read at all, for two unrelated reasons.
type SubscriberStats struct {
	// Overruns is how often the ring overtook this subscriber.
	Overruns int64
	// DroppedBytes is what the ring discarded before this subscriber read it.
	DroppedBytes int64
	// ResyncSkippedBytes is what recovery skipped on top of that to reach a random
	// access point. It is never added to DroppedBytes: one is pressure on the ring,
	// the other is a deliberate correction.
	ResyncSkippedBytes int64
	// LagBytes is how far behind the write head this subscriber currently is.
	LagBytes int64
}

// Stats returns a snapshot of this subscriber's accounting. Reading the individual
// accessors instead would take the lock once per value and could pair a lag with
// byte counters from a different moment.
func (s *SubscriberReader) Stats() SubscriberStats {
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()

	lag := s.ring.head - s.readOffset
	if lag < 0 {
		lag = 0
	}
	return SubscriberStats{
		Overruns:           s.overruns,
		DroppedBytes:       s.droppedBytes,
		ResyncSkippedBytes: s.resyncSkippedBytes,
		LagBytes:           lag,
	}
}

// DroppedBytes returns the bytes the ring discarded before this subscriber read
// them. It does not include bytes recovery deliberately skipped to reach a random
// access point - those are reported by ResyncSkippedBytes.
func (s *SubscriberReader) DroppedBytes() int64 {
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()
	return s.droppedBytes
}

// ResyncSkippedBytes returns the bytes that were still held by the ring but were
// skipped to re-enter at a random access point after an overrun.
func (s *SubscriberReader) ResyncSkippedBytes() int64 {
	s.ring.mu.Lock()
	defer s.ring.mu.Unlock()
	return s.resyncSkippedBytes
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
