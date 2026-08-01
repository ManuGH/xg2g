// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Artifact represents an HLS playlist or segment stored in RAM.
type Artifact struct {
	Filename string
	Data     []byte
	ModTime  time.Time
}

// DVRCallback is invoked asynchronously when a new chunk is ingested.
type DVRCallback func(sessionID, filename string, data []byte)

// Buffer manages an in-memory ring buffer of HLS segments and playlists for a live session.
type Buffer struct {
	sessionID   string
	serviceRef  string
	maxSegments int
	mu          sync.RWMutex
	segments    []string // ordered slice of complete segment filenames (seg_*)
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
	return NewBufferWithStore(sessionID, sessionID, maxSegments, dvrCb, nil, nil)
}

// NewBufferWithStore creates a new ring buffer bound to an authoritative ReservationStore.
func NewBufferWithStore(serviceRef, sessionID string, maxSegments int, dvrCb DVRCallback, store *ReservationStore, index *SegmentIndex) *Buffer {
	if maxSegments <= 0 {
		maxSegments = 20
	}
	b := &Buffer{
		serviceRef:  serviceRef,
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
	b.mu.RLock()
	cb := b.dvrCb
	b.mu.RUnlock()
	for art := range b.dvrCh {
		if cb != nil {
			cb(b.sessionID, art.Filename, art.Data)
		}
	}
}

// ParseSegmentFilename cleanly parses seg_%d.ts or part_%d_%d.ts without falling back to 0.
func ParseSegmentFilename(serviceRef, sessionID, filename string) (SegmentID, bool) {
	if strings.HasPrefix(filename, "seg_") {
		trimmed := strings.TrimPrefix(filename, "seg_")
		dotIdx := strings.Index(trimmed, ".")
		if dotIdx > 0 {
			trimmed = trimmed[:dotIdx]
		}
		if s, err := strconv.ParseUint(trimmed, 10, 64); err == nil {
			return SegmentID{
				ServiceRef: serviceRef,
				SessionID:  sessionID,
				Kind:       SegmentKindComplete,
				Sequence:   s,
				PartIndex:  0,
			}, true
		}
	} else if strings.HasPrefix(filename, "part_") {
		trimmed := strings.TrimPrefix(filename, "part_")
		dotIdx := strings.Index(trimmed, ".")
		if dotIdx > 0 {
			trimmed = trimmed[:dotIdx]
		}
		parts := strings.Split(trimmed, "_")
		if len(parts) == 2 {
			s, err1 := strconv.ParseUint(parts[0], 10, 64)
			p, err2 := strconv.ParseUint(parts[1], 10, 32)
			if err1 == nil && err2 == nil {
				return SegmentID{
					ServiceRef: serviceRef,
					SessionID:  sessionID,
					Kind:       SegmentKindPart,
					Sequence:   s,
					PartIndex:  uint32(p),
				}, true
			}
		}
	}
	return SegmentID{}, false
}

// Put adds or replaces an artifact in the ring buffer.
func (b *Buffer) Put(filename string, data []byte) {
	b.PutWithMetadata(filename, data, SegmentMetadata{})
}

// PutWithMetadata adds a segment with extracted media duration, PTS, and discontinuity metadata.
func (b *Buffer) PutWithMetadata(filename string, data []byte, meta SegmentMetadata) {
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

	segID, isSegment := ParseSegmentFilename(b.serviceRef, b.sessionID, filename)
	// Variant A: Retro-DVR ONLY indexes and counts COMPLETE segments (seg_*)
	if isSegment && segID.Kind == SegmentKindComplete && !exists {
		b.segments = append(b.segments, filename)

		durSec := meta.Duration.Seconds()
		if durSec <= 0 {
			durSec = 2.0 // Fallback
		}

		startWall := art.ModTime
		if meta.ProgramTime != nil {
			startWall = *meta.ProgramTime
		}
		endWall := startWall.Add(time.Duration(durSec * float64(time.Second)))

		// Register segment with authoritative store
		if b.store != nil {
			b.store.mu.Lock()
			b.index.AddSegment(&InternalSegment{
				ID: segID,
				Location: SegmentLocation{
					Kind:     StorageKindRAM,
					Filename: filename,
				},
				DurationSec:   durSec,
				Sequence:      segID.Sequence,
				StartPTS90k:   meta.StartPTS90k,
				EndPTS90k:     meta.EndPTS90k,
				PTSEpoch:      meta.PTSEpoch,
				StartWallTime: startWall,
				EndWallTime:   endWall,
				SizeBytes:     int64(len(data)),
				Discontinuity: meta.Discontinuity,
				CodecHash:     meta.CodecHash,
				State:         SegmentActive,
				Data:          data,
			})
			b.store.mu.Unlock()
		}

		var keptSegments []string
		for idx, s := range b.segments {
			if len(b.segments)-idx <= b.maxSegments {
				keptSegments = append(keptSegments, s)
				continue
			}

			candID, candOk := ParseSegmentFilename(b.serviceRef, b.sessionID, s)
			if candOk && b.store != nil {
				b.store.mu.Lock()
				canEvict := b.index.TryMarkDeleting(candID)
				if !canEvict {
					// Reserved! Keep in buffer
					b.store.mu.Unlock()
					keptSegments = append(keptSegments, s)
					continue
				}
				b.index.RemoveSegment(candID)
				b.store.mu.Unlock()
			}

			delete(b.artifacts, s)
		}
		b.segments = keptSegments
	}

	if b.dvrCh != nil {
		select {
		case b.dvrCh <- art:
		default:
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

// ByteSnapshot returns an immutable byte slice copy of a RAM artifact for staging handover.
func (b *Buffer) ByteSnapshot(filename string) ([]byte, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	art, ok := b.artifacts[filename]
	if !ok || len(art.Data) == 0 {
		return nil, false
	}
	cp := make([]byte, len(art.Data))
	copy(cp, art.Data)
	return cp, true
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
	reg, _ := NewRegistryWithStorage(maxSegments, "")
	return reg
}

// NewRegistryWithStorage initializes a Registry with persistent storage for reservations.
func NewRegistryWithStorage(maxSegments int, storagePath string) (*Registry, error) {
	r := &Registry{
		buffers:     make(map[string]*Buffer),
		maxSegments: maxSegments,
		closeCh:     make(chan struct{}),
	}
	r.index = NewSegmentIndex()
	r.store = NewReservationStore(r.index, DefaultReservationLimits(), storagePath)
	r.lifecycle = NewLifecycleManager(r.store, r.index)

	if err := r.lifecycle.RunRecovery(); err != nil && storagePath != "" {
		return nil, fmt.Errorf("failed to recover ringbuffer state: %w", err)
	}

	go r.cleanupLoop()
	return r, nil
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

// GetOrCreate returns an existing buffer or creates a new one.
func (r *Registry) GetOrCreate(sessionID string, dvrCb DVRCallback) *Buffer {
	return r.GetOrCreateService(sessionID, sessionID, dvrCb)
}

// GetOrCreateService returns an existing buffer or creates a new one bound to serviceRef.
func (r *Registry) GetOrCreateService(serviceRef, sessionID string, dvrCb DVRCallback) *Buffer {
	r.mu.Lock()
	defer r.mu.Unlock()
	buf, ok := r.buffers[sessionID]
	if !ok {
		buf = NewBufferWithStore(serviceRef, sessionID, r.maxSegments, dvrCb, r.store, r.index)
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

// Delete removes and closes the buffer for sessionID if not reserved.
func (r *Registry) Delete(sessionID string) {
	r.mu.Lock()
	if r.store != nil && r.store.HasReservationsForSession(sessionID) {
		r.mu.Unlock()
		return // DO NOT DELETE SESSION BUFFER IF RESERVED!
	}
	buf, ok := r.buffers[sessionID]
	if ok {
		delete(r.buffers, sessionID)
	}
	r.mu.Unlock()
	if ok {
		buf.Close()
	}
}

// Stop shuts down the cleanup loop and underlying reservation reaper cleanly.
func (r *Registry) Stop() {
	r.closeOnce.Do(func() {
		close(r.closeCh)
		if r.store != nil {
			r.store.Close()
		}
	})
}

func (r *Registry) cleanupLoop() {
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
					// DO NOT DELETE SESSION BUFFER IF IT CONTAINS RESERVED SEGMENTS!
					if r.store != nil && r.store.HasReservationsForSession(id) {
						continue
					}
					delete(r.buffers, id)
					buf.Close()
				}
			}
			r.mu.Unlock()
		}
	}
}
