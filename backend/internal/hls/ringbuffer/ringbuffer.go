// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"fmt"
	"sync"
	"time"
)

// Artifact represents an HLS playlist or segment stored in RAM.
type Artifact struct {
	Filename string
	Data     []byte
	ModTime  time.Time
}

// DVRCallback is invoked asynchronously when a new chunk is ingested,
// allowing disk archiving without blocking the real-time live streaming path.
type DVRCallback func(sessionID, filename string, data []byte)

// Buffer manages an in-memory ring buffer of HLS segments and playlists for a single live session.
type Buffer struct {
	sessionID   string
	maxSegments int
	mu          sync.RWMutex
	segments    []string // ordered slice of segment filenames
	artifacts   map[string]*Artifact
	lastUpdated time.Time
	dvrCb       DVRCallback
	dvrCh       chan *Artifact
	closed      bool
	store       *ReservationStore
	index       *SegmentIndex
}

// NewBuffer creates a new ring buffer for a session.
func NewBuffer(sessionID string, maxSegments int, dvrCb DVRCallback) *Buffer {
	return NewBufferWithStore(sessionID, maxSegments, dvrCb, nil, nil)
}

// NewBufferWithStore creates a new ring buffer bound to an authoritative ReservationStore.
func NewBufferWithStore(sessionID string, maxSegments int, dvrCb DVRCallback, store *ReservationStore, index *SegmentIndex) *Buffer {
	if maxSegments <= 0 {
		maxSegments = 20
	}
	b := &Buffer{
		sessionID:   sessionID,
		maxSegments: maxSegments,
		artifacts:   make(map[string]*Artifact),
		lastUpdated: time.Now(),
		dvrCb:       dvrCb,
		store:       store,
		index:       index,
	}
	if dvrCb != nil {
		b.dvrCh = make(chan *Artifact, 100)
		go b.dvrWorker()
	}
	return b
}

func (b *Buffer) dvrWorker() {
	// Capture dvrCb once under read lock to formally satisfy the race detector,
	// even though happens-before guarantees make it safe in practice.
	b.mu.RLock()
	cb := b.dvrCb
	b.mu.RUnlock()
	for art := range b.dvrCh {
		if cb != nil {
			cb(b.sessionID, art.Filename, art.Data)
		}
	}
}

// Put adds or replaces an artifact in the ring buffer. If the artifact is a segment
// (e.g. seg_000001.ts or seg_000001.m4s) and the buffer exceeds maxSegments, the oldest segment is evicted unless reserved.
func (b *Buffer) Put(filename string, data []byte) {
	art := &Artifact{
		Filename: filename,
		Data:     data,
		ModTime:  time.Now(),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.lastUpdated = art.ModTime
	_, exists := b.artifacts[filename]
	b.artifacts[filename] = art

	isSegment := len(filename) >= 4 && (filename[:4] == "seg_" || filename[:5] == "part_")
	if isSegment && !exists {
		b.segments = append(b.segments, filename)

		// Parse sequence number if possible
		var seq uint64
		if len(filename) > 4 && filename[:4] == "seg_" {
			_, _ = fmt.Sscanf(filename, "seg_%d", &seq)
		}

		segID := SegmentID{
			ServiceRef: b.sessionID,
			SessionID:  b.sessionID,
			Sequence:   seq,
		}

		if b.index != nil {
			b.index.AddSegment(&InternalSegment{
				ID:            segID,
				Path:          filename,
				Sequence:      seq,
				StartWallTime: art.ModTime,
				EndWallTime:   art.ModTime.Add(2 * time.Second),
				SizeBytes:     int64(len(data)),
				State:         SegmentActive,
			})
		}

		var keptSegments []string
		for idx, s := range b.segments {
			if len(b.segments)-idx <= b.maxSegments {
				keptSegments = append(keptSegments, s)
				continue
			}

			// Parse sequence for candidate eviction
			var candidateSeq uint64
			if len(s) > 4 && s[:4] == "seg_" {
				_, _ = fmt.Sscanf(s, "seg_%d", &candidateSeq)
			}
			candID := SegmentID{
				ServiceRef: b.sessionID,
				SessionID:  b.sessionID,
				Sequence:   candidateSeq,
			}

			// If segment is reserved in index, DO NOT EVICT!
			if b.index != nil {
				if seg, ok := b.index.GetByID(candID); ok && seg.IsReserved() {
					keptSegments = append(keptSegments, s)
					continue
				}
				b.index.MarkForDeletion(candID)
			}

			delete(b.artifacts, s)
		}
		b.segments = keptSegments
	}

	// Send to DVR channel while holding the lock so Close() cannot race
	// by closing the channel between the check and the send.
	if b.dvrCh != nil {
		select {
		case b.dvrCh <- art:
		default:
			// If DVR writer is overwhelmed, we don't block the live stream ingest
		}
	}
	b.mu.Unlock()
}

// Get retrieves an artifact by filename.
func (b *Buffer) Get(filename string) (*Artifact, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	art, ok := b.artifacts[filename]
	return art, ok
}

// Close shuts down the buffer and its background DVR worker.
func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		if b.dvrCh != nil {
			close(b.dvrCh)
		}
	}
}

// Registry manages all active in-memory session ring buffers.
type Registry struct {
	mu          sync.RWMutex
	buffers     map[string]*Buffer
	maxSegments int
	closeCh     chan struct{}
	closeOnce   sync.Once
	store       *ReservationStore
	index       *SegmentIndex
	lifecycle   *LifecycleManager
}

// NewRegistry initializes a Registry.
func NewRegistry(maxSegments int) *Registry {
	return NewRegistryWithStorage(maxSegments, "")
}

// NewRegistryWithStorage initializes a Registry with a persistent storage location for reservations.
func NewRegistryWithStorage(maxSegments int, storagePath string) *Registry {
	r := &Registry{
		buffers:     make(map[string]*Buffer),
		maxSegments: maxSegments,
		closeCh:     make(chan struct{}),
	}
	r.index = NewSegmentIndex()
	r.store = NewReservationStore(r.index, DefaultReservationLimits(), storagePath)
	r.lifecycle = NewLifecycleManager(r.store, r.index)
	_ = r.lifecycle.RunRecovery()
	go r.cleanupLoop()
	return r
}

// DefaultRegistry is the global singleton registry for live sessions.
var DefaultRegistry = NewRegistry(20)

// Store returns the underlying ReservationStore.
func (r *Registry) Store() *ReservationStore {
	return r.store
}

// Index returns the underlying SegmentIndex.
func (r *Registry) Index() *SegmentIndex {
	return r.index
}

// GetOrCreate returns an existing buffer or creates a new one for sessionID.
func (r *Registry) GetOrCreate(sessionID string, dvrCb DVRCallback) *Buffer {
	r.mu.Lock()
	defer r.mu.Unlock()
	buf, ok := r.buffers[sessionID]
	if !ok {
		buf = NewBufferWithStore(sessionID, r.maxSegments, dvrCb, r.store, r.index)
		r.buffers[sessionID] = buf
	} else if dvrCb != nil && buf.dvrCb == nil {
		buf.mu.Lock()
		buf.dvrCb = dvrCb
		if buf.dvrCh == nil {
			buf.dvrCh = make(chan *Artifact, 100)
			go buf.dvrWorker()
		}
		buf.mu.Unlock()
	}
	return buf
}

// Get retrieves the buffer for sessionID if it exists.
func (r *Registry) Get(sessionID string) (*Buffer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	buf, ok := r.buffers[sessionID]
	return buf, ok
}

// Delete removes and closes the buffer for sessionID.
func (r *Registry) Delete(sessionID string) {
	r.mu.Lock()
	buf, ok := r.buffers[sessionID]
	if ok {
		delete(r.buffers, sessionID)
	}
	r.mu.Unlock()
	if ok {
		buf.Close()
	}
}

// Stop shuts down the cleanup loop. Once stopped, the Registry must not be reused.
func (r *Registry) Stop() {
	r.closeOnce.Do(func() {
		close(r.closeCh)
	})
}

func (r *Registry) cleanupLoop() {
	// Block cleanup until startup recovery lifecycle reaches CLEANUP_ENABLED
	r.lifecycle.WaitCleanupEnabled()

	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-r.closeCh:
			return
		case <-ticker.C:
			now := time.Now()
			r.mu.Lock()
			for id, buf := range r.buffers {
				buf.mu.RLock()
				last := buf.lastUpdated
				buf.mu.RUnlock()
				if now.Sub(last) > 10*time.Minute {
					delete(r.buffers, id)
					buf.Close()
				}
			}
			r.mu.Unlock()
		}
	}
}
