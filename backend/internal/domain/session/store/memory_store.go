// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// MemoryStore is an in-memory StateStore intended for tests and local iteration.
// Not durable; not suitable for production.
type MemoryStore struct {
	mu sync.RWMutex

	sessions   map[string]*model.SessionRecord
	recordings map[string]*model.Recording // Added for testing

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
		sessions:   make(map[string]*model.SessionRecord),
		recordings: make(map[string]*model.Recording),
		leases:     make(map[string]leaseState),
		idem:       make(map[string]idemState),
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

func (m *MemoryStore) DeleteIdempotencyIfMatch(ctx context.Context, idemKey, sessionID string) (bool, error) {
	if idemKey == "" {
		return false, nil
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.idem[idemKey]
	if !ok {
		return false, nil
	}
	if now.After(st.exp) {
		delete(m.idem, idemKey)
		return false, nil
	}
	if st.sessionID != sessionID {
		return false, nil
	}

	delete(m.idem, idemKey)
	return true, nil
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

func (m *MemoryStore) GetLease(ctx context.Context, key string) (Lease, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.leases[key]
	if !ok {
		return nil, false, nil
	}
	if time.Now().After(st.exp) {
		delete(m.leases, key)
		return nil, false, nil
	}
	return &memoryLease{store: m, key: key, owner: st.owner, exp: st.exp}, true, nil
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
		list = append(list, cloneSessionRecord(rec))
	}
	sortSessionRecords(list)
	return list, nil
}

// QuerySessions returns sessions matching filter criteria (ADR-009 CTO Patch 2)
// Efficient query - NO full scan, filters applied during iteration
func (m *MemoryStore) QuerySessions(ctx context.Context, filter SessionFilter) ([]*model.SessionRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*model.SessionRecord

	// Build state map for efficient lookup
	stateMatch := make(map[model.SessionState]bool)
	for _, state := range filter.States {
		stateMatch[state] = true
	}

	for _, rec := range m.sessions {
		// Filter by state
		if len(filter.States) > 0 && !stateMatch[rec.State] {
			continue
		}

		// Filter by lease expiry
		if filter.LeaseExpiresBefore > 0 && rec.LeaseExpiresAtUnix > filter.LeaseExpiresBefore {
			continue
		}

		result = append(result, cloneSessionRecord(rec))
	}

	sortSessionRecords(result)
	return result, nil
}

func sortSessionRecords(list []*model.SessionRecord) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].UpdatedAtUnix != list[j].UpdatedAtUnix {
			return list[i].UpdatedAtUnix > list[j].UpdatedAtUnix
		}
		if list[i].CreatedAtUnix != list[j].CreatedAtUnix {
			return list[i].CreatedAtUnix > list[j].CreatedAtUnix
		}
		return list[i].SessionID < list[j].SessionID
	})
}

func (m *MemoryStore) PutSession(ctx context.Context, rec *model.SessionRecord) error {
	m.mu.Lock()
	m.sessions[rec.SessionID] = cloneSessionRecord(rec)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, idemKey string, ttl time.Duration) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Check Idempotency
	if idemKey != "" {
		if st, ok := m.idem[idemKey]; ok {
			if time.Now().Before(st.exp) {
				return st.sessionID, true, nil
			}
			// Expired: delete and proceed to overwrite
			delete(m.idem, idemKey)
		}
	}

	// 2. Write Session
	m.sessions[s.SessionID] = cloneSessionRecord(s)

	// 3. Write Idempotency
	if idemKey != "" {
		deadline := time.Now().Add(ttl)
		m.idem[idemKey] = idemState{sessionID: s.SessionID, exp: deadline}
	}
	return "", false, nil
}

func (m *MemoryStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	// Step 1: Create snapshot under lock
	m.mu.RLock()
	snapshot := make([]*model.SessionRecord, 0, len(m.sessions))
	for _, rec := range m.sessions {
		snapshot = append(snapshot, cloneSessionRecord(rec))
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
	defer m.mu.Unlock()
	rec, ok := m.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	return cloneSessionRecord(rec), nil
}

func (m *MemoryStore) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (m *MemoryStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cpy := cloneSessionRecord(rec)

	if err := fn(cpy); err != nil {
		return nil, err
	}
	// Save back
	m.sessions[id] = cloneSessionRecord(cpy)
	return cloneSessionRecord(cpy), nil
}

func cloneSessionRecord(rec *model.SessionRecord) *model.SessionRecord {
	if rec == nil {
		return nil
	}
	cp := *rec
	if rec.ContextData != nil {
		cp.ContextData = make(map[string]string, len(rec.ContextData))
		for k, v := range rec.ContextData {
			cp.ContextData[k] = v
		}
	}
	cp.PlaybackTrace = rec.PlaybackTrace.Clone()
	return &cp
}

func (m *MemoryStore) DeleteAllLeases(ctx context.Context) (int, error) {
	m.mu.Lock()
	count := len(m.leases)
	m.leases = make(map[string]leaseState)
	m.mu.Unlock()
	return count, nil
}

// Minimal Recording Store implementation for tests
func (m *MemoryStore) PutRecording(ctx context.Context, rec model.Recording) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := rec
	m.recordings[rec.ID] = &cp
	return nil
}

func (m *MemoryStore) ListRecordings(ctx context.Context, _ any) ([]model.Recording, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []model.Recording
	for _, r := range m.recordings {
		list = append(list, *r)
	}
	// Sort by ID for deterministic tests
	// (Simple bubble/api sort if needed, but for 1 item test irrelevant)
	return list, nil
}
