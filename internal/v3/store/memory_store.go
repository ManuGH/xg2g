// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/model"
)

// MemoryStore is an in-memory StateStore intended for tests and local iteration.
// Not durable; not suitable for production.
type MemoryStore struct {
	mu sync.RWMutex

	sessions  map[string]*model.SessionRecord
	pipelines map[string]*model.PipelineRecord

	// key -> lease state
	leases map[string]leaseState

	// idemKey -> sessionID (with expiry)
	idem map[string]idemState
}

type leaseState struct {
	owner string
	exp   time.Time
}

type idemState struct {
	sessionID string
	exp       time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:  make(map[string]*model.SessionRecord),
		pipelines: make(map[string]*model.PipelineRecord),
		leases:    make(map[string]leaseState),
		idem:      make(map[string]idemState),
	}
}

func (m *MemoryStore) Close() error { return nil }

func (m *MemoryStore) PutIdempotency(ctx context.Context, idemKey, sessionID string, ttl time.Duration) error {
	if idemKey == "" {
		return nil
	}
	deadline := time.Now().Add(ttl)
	m.mu.Lock()
	m.idem[idemKey] = idemState{sessionID: sessionID, exp: deadline}
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) GetIdempotency(ctx context.Context, idemKey string) (string, bool, error) {
	if idemKey == "" {
		return "", false, nil
	}
	now := time.Now()
	m.mu.Lock()
	st, ok := m.idem[idemKey]
	if ok && now.After(st.exp) {
		delete(m.idem, idemKey)
		ok = false
	}
	m.mu.Unlock()
	if !ok {
		return "", false, nil
	}
	return st.sessionID, true, nil
}

func (m *MemoryStore) TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
	now := time.Now()
	deadline := now.Add(ttl)
	m.mu.Lock()
	ls, ok := m.leases[key]
	if ok && now.After(ls.exp) {
		delete(m.leases, key)
		ok = false
	}
	if ok {
		if ls.owner == owner {
			// Re-entry: Update expiration (renew)
			ls.exp = deadline
			m.leases[key] = ls
			m.mu.Unlock()
			return &memoryLease{store: m, key: key, owner: owner, ttl: ttl, exp: deadline}, true, nil
		}
		m.mu.Unlock()
		return nil, false, nil
	}
	m.leases[key] = leaseState{owner: owner, exp: deadline}
	m.mu.Unlock()
	return &memoryLease{store: m, key: key, owner: owner, ttl: ttl, exp: deadline}, true, nil
}

type memoryLease struct {
	store *MemoryStore
	key   string
	owner string
	ttl   time.Duration
	exp   time.Time
}

func (m *MemoryStore) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
	if ttl <= 0 {
		return nil, false, errors.New("invalid ttl")
	}
	now := time.Now()
	exp := now.Add(ttl)
	m.mu.Lock()
	st, ok := m.leases[key]
	if !ok || st.owner != owner {
		m.mu.Unlock()
		return nil, false, nil // Lost lease
	}
	st.exp = exp
	m.leases[key] = st
	m.mu.Unlock()
	return &memoryLease{store: m, key: key, owner: owner, ttl: ttl, exp: exp}, true, nil
}

func (m *MemoryStore) ReleaseLease(ctx context.Context, key, owner string) error {
	m.mu.Lock()
	st, ok := m.leases[key]
	if ok && st.owner == owner {
		delete(m.leases, key)
	}
	m.mu.Unlock()
	return nil
}

func (l *memoryLease) Key() string          { return l.key }
func (l *memoryLease) Owner() string        { return l.owner }
func (l *memoryLease) ExpiresAt() time.Time { return l.exp }

// ListSessions returns all sessions (Debug/Admin).
func (m *MemoryStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*model.SessionRecord
	for _, rec := range m.sessions {
		// Return copy
		cp := *rec
		list = append(list, &cp)
	}
	return list, nil
}

func (m *MemoryStore) PutSession(ctx context.Context, rec *model.SessionRecord) error {
	m.mu.Lock()
	cpy := *rec
	m.sessions[rec.SessionID] = &cpy
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, idemKey string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Write Session
	cpy := *s
	m.sessions[s.SessionID] = &cpy

	// 2. Write Idempotency
	if idemKey != "" {
		deadline := time.Now().Add(ttl)
		m.idem[idemKey] = idemState{sessionID: s.SessionID, exp: deadline}
	}
	return nil
}

func (m *MemoryStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	// Step 1: Create snapshot under lock
	m.mu.RLock()
	snapshot := make([]*model.SessionRecord, 0, len(m.sessions))
	for _, rec := range m.sessions {
		cpy := *rec // Deep copy
		snapshot = append(snapshot, &cpy)
	}
	m.mu.RUnlock()

	// Step 2: Iterate without lock - prevents blocking reads during slow callbacks
	for _, rec := range snapshot {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := fn(rec); err != nil {
			return err
		}
	}

	return nil
}

func (m *MemoryStore) GetSession(ctx context.Context, sessionID string) (*model.SessionRecord, error) {
	m.mu.Lock()
	rec, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil, nil
	}
	cpy := *rec
	m.mu.Unlock()
	return &cpy, nil
}

func (m *MemoryStore) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (m *MemoryStore) PutPipeline(ctx context.Context, rec *model.PipelineRecord) error {
	m.mu.Lock()
	cpy := *rec
	m.pipelines[rec.PipelineID] = &cpy
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) GetPipeline(ctx context.Context, pipelineID string) (*model.PipelineRecord, error) {
	m.mu.Lock()
	rec, ok := m.pipelines[pipelineID]
	if !ok {
		m.mu.Unlock()
		return nil, nil
	}
	cpy := *rec
	m.mu.Unlock()
	return &cpy, nil
}

func (m *MemoryStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	// Copy to work on
	cpy := *rec
	if err := fn(&cpy); err != nil {
		return nil, err
	}
	// Save back
	m.sessions[id] = &cpy
	return &cpy, nil
}

func (m *MemoryStore) UpdatePipeline(ctx context.Context, id string, fn func(*model.PipelineRecord) error) (*model.PipelineRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.pipelines[id]
	if !ok {
		return nil, errors.New("not found")
	}
	// Copy
	cpy := *rec
	if err := fn(&cpy); err != nil {
		return nil, err
	}
	m.pipelines[id] = &cpy
	return &cpy, nil
}

func (m *MemoryStore) DeleteAllLeases(ctx context.Context) (int, error) {
	m.mu.Lock()
	count := len(m.leases)
	m.leases = make(map[string]leaseState)
	m.mu.Unlock()
	return count, nil
}
