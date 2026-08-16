// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
)

// MemoryEnrichmentStore is an in-memory, thread-safe implementation of EnrichmentStore
// intended for unit tests and local non-persistent deployments.
type MemoryEnrichmentStore struct {
	mu      sync.RWMutex
	records map[string]*epg.EnrichmentData
	closed  bool
}

// NewMemoryEnrichmentStore creates a new in-memory store.
func NewMemoryEnrichmentStore() *MemoryEnrichmentStore {
	return &MemoryEnrichmentStore{
		records: make(map[string]*epg.EnrichmentData),
	}
}

func (s *MemoryEnrichmentStore) Get(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, false, nil
	}

	key := fp.Key()
	rec, exists := s.records[key]
	if !exists || rec == nil {
		return nil, false, nil
	}

	now := time.Now()
	if rec.IsExpired(now) {
		return nil, false, nil
	}

	if rec.MatcherVersion != epg.CurrentMatcherVersion {
		return nil, false, nil
	}

	// Return a copy to prevent external mutation
	copied := *rec
	return &copied, true, nil
}

func (s *MemoryEnrichmentStore) Put(ctx context.Context, data *epg.EnrichmentData) error {
	if data == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	// Store copy
	copied := *data
	s.records[data.FingerprintKey] = &copied
	return nil
}

func (s *MemoryEnrichmentStore) PruneExpired(ctx context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, nil
	}

	var pruned int64
	for k, v := range s.records {
		if v.IsExpired(now) {
			delete(s.records, k)
			pruned++
		}
	}
	return pruned, nil
}

func (s *MemoryEnrichmentStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	s.records = make(map[string]*epg.EnrichmentData)
	return nil
}
