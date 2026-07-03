// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ringbuffer

import (
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
}

// NewBuffer creates a new ring buffer for a session.
func NewBuffer(sessionID string, maxSegments int, dvrCb DVRCallback) *Buffer {
	if maxSegments <= 0 {
		maxSegments = 20
	}
	b := &Buffer{
		sessionID:   sessionID,
		maxSegments: maxSegments,
		artifacts:   make(map[string]*Artifact),
		lastUpdated: time.Now(),
		dvrCb:       dvrCb,
	}
	if dvrCb != nil {
		b.dvrCh = make(chan *Artifact, 100)
		go b.dvrWorker()
	}
	return b
}

func (b *Buffer) dvrWorker() {
	for art := range b.dvrCh {
		if b.dvrCb != nil {
			b.dvrCb(b.sessionID, art.Filename, art.Data)
		}
	}
}

// Put adds or replaces an artifact in the ring buffer. If the artifact is a segment
// (e.g. seg_000001.ts or seg_000001.m4s) and the buffer exceeds maxSegments, the oldest segment is evicted.
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
		for len(b.segments) > b.maxSegments {
			oldest := b.segments[0]
			b.segments = b.segments[1:]
			delete(b.artifacts, oldest)
		}
	}
	dvrCh := b.dvrCh
	b.mu.Unlock()

	if dvrCh != nil {
		select {
		case dvrCh <- art:
		default:
			// If DVR writer is overwhelmed, we don't block the live stream ingest
		}
	}
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
}

// NewRegistry initializes a Registry.
func NewRegistry(maxSegments int) *Registry {
	r := &Registry{
		buffers:     make(map[string]*Buffer),
		maxSegments: maxSegments,
	}
	go r.cleanupLoop()
	return r
}

// DefaultRegistry is the global singleton registry for live sessions.
var DefaultRegistry = NewRegistry(20)

// GetOrCreate returns an existing buffer or creates a new one for sessionID.
func (r *Registry) GetOrCreate(sessionID string, dvrCb DVRCallback) *Buffer {
	r.mu.Lock()
	defer r.mu.Unlock()
	buf, ok := r.buffers[sessionID]
	if !ok {
		buf = NewBuffer(sessionID, r.maxSegments, dvrCb)
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

func (r *Registry) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	for range ticker.C {
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
