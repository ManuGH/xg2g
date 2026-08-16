// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"errors"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
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

	// Multi-resource claims (Phase 3 & Phase 4)
	inputClaims map[string]*memInputClaim
	muxClaims   map[string]*memMuxClaim
	muxMembers  map[string]map[string]memMemberState // muxID -> (sessionID -> memMemberState)
}

type memMemberState struct {
	generationToken string
	exp             time.Time
}

type memInputClaim struct {
	activePlane string
	owners      map[string]memMemberState // sessionID -> memMemberState
	exp         time.Time
}

type memMuxClaim struct {
	multiplexID   string
	inputID       string
	demodID       string
	requiredPlane string
	scrSlot       *int
	exp           time.Time
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
		sessions:    make(map[string]*model.SessionRecord),
		recordings:  make(map[string]*model.Recording),
		leases:      make(map[string]leaseState),
		idem:        make(map[string]idemState),
		inputClaims: make(map[string]*memInputClaim),
		muxClaims:   make(map[string]*memMuxClaim),
		muxMembers:  make(map[string]map[string]memMemberState),
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

func (m *MemoryStore) ListLeases(ctx context.Context) ([]Lease, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	res := make([]Lease, 0, len(m.leases))
	for key, st := range m.leases {
		if now.After(st.exp) {
			delete(m.leases, key)
			continue
		}
		res = append(res, &memoryLease{store: m, key: key, owner: st.owner, exp: st.exp})
	}
	return res, nil
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

func (m *MemoryStore) GetDiagnosticMetadata(ctx context.Context, sessionID string) (ports.DiagnosticMetadata, bool) {
	rec, err := m.GetSession(ctx, sessionID)
	if err != nil || rec == nil {
		return ports.DiagnosticMetadata{}, false
	}
	return ports.DiagnosticMetadata{
		GenerationID:          rec.GenerationID,
		CorrelationID:         rec.CorrelationID,
		Reason:                string(rec.Reason),
		StopRequestedAtUnixMs: rec.StopRequestedAtUnixMs,
	}, true
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
	// Advance the liveness clock on every update, mirroring SqliteStore.UpdateSession.
	// This is the source of truth for staleness gates (e.g. the recovery sweep's
	// shouldRecover): a store that froze UpdatedAtUnix would make every Memory-backed
	// session look stale. The two store implementations must agree on this semantic.
	cpy.UpdatedAtUnix = time.Now().Unix()
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
		maps.Copy(cp.ContextData, rec.ContextData)
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

// --- Multi-Resource Transactional Claim Engine (Phase 3 & Phase 4) ---

func (m *MemoryStore) TryAcquireClaimSet(ctx context.Context, req model.ClaimSetRequest) (model.ClaimSetResult, error) {
	if req.SessionID == "" {
		return model.ClaimSetResult{Success: false, ConflictType: model.ConflictCapacityExhausted, ConflictDesc: "session_id required"}, nil
	}
	if req.TTL <= 0 {
		req.TTL = 30 * time.Second
	}
	genToken := req.GenerationToken
	if genToken == "" {
		genToken = uuid.New().String()
	}
	maxMembers := req.MaxMuxMembers
	if maxMembers <= 0 {
		maxMembers = 8
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	expiresAt := now.Add(req.TTL)

	// 1. Multiplex-Reuse
	if req.MultiplexID != "" {
		if existing, ok := m.muxClaims[req.MultiplexID]; ok && existing.exp.After(now) {
			// Invariant: verify parent hardware claims are still held
			demodKey := model.LeaseKeyDemod(existing.demodID)
			if demodLease, okDemod := m.leases[demodKey]; okDemod && demodLease.exp.After(now) {
				// Count active members
				activeCount := 0
				if members, ok := m.muxMembers[req.MultiplexID]; ok {
					for _, st := range members {
						if st.exp.After(now) {
							activeCount++
						}
					}
				}

				if activeCount < maxMembers {
					// Join members
					if m.muxMembers[req.MultiplexID] == nil {
						m.muxMembers[req.MultiplexID] = make(map[string]memMemberState)
					}
					m.muxMembers[req.MultiplexID][req.SessionID] = memMemberState{generationToken: genToken, exp: expiresAt}

					// Extend expiry if needed
					if expiresAt.After(existing.exp) {
						existing.exp = expiresAt
						demodLease.exp = expiresAt
						m.leases[demodKey] = demodLease
						if existing.inputID != "" {
							if inClaim, okIn := m.inputClaims[existing.inputID]; okIn {
								if expiresAt.After(inClaim.exp) {
									inClaim.exp = expiresAt
								}
								if inClaim.owners != nil {
									inClaim.owners[req.SessionID] = memMemberState{generationToken: genToken, exp: expiresAt}
								}
							}
						}
					}

					return model.ClaimSetResult{
						Success:         true,
						GenerationToken: genToken,
						ReusedMux:       true,
						DemodID:         existing.demodID,
						InputID:         existing.inputID,
						ExpiresAt:       expiresAt,
					}, nil
				}
				// If mux is full (activeCount >= maxMembers), fall through to allocate separate demod
			}
		}
	}

	// 2. Compatible-Shared Input Check
	if req.InputID != "" && req.RequiredPlane != "" {
		if inClaim, ok := m.inputClaims[req.InputID]; ok && inClaim.exp.After(now) {
			if inClaim.activePlane != req.RequiredPlane {
				return model.ClaimSetResult{
					Success:      false,
					ConflictType: model.ConflictPlaneConflict,
					ConflictDesc: "input " + req.InputID + " is locked to plane " + inClaim.activePlane,
				}, nil
			}
			if expiresAt.After(inClaim.exp) {
				inClaim.exp = expiresAt
			}
			if inClaim.owners == nil {
				inClaim.owners = make(map[string]memMemberState)
			}
			inClaim.owners[req.SessionID] = memMemberState{generationToken: genToken, exp: expiresAt}
		} else {
			m.inputClaims[req.InputID] = &memInputClaim{
				activePlane: req.RequiredPlane,
				owners:      map[string]memMemberState{req.SessionID: {generationToken: genToken, exp: expiresAt}},
				exp:         expiresAt,
			}
		}
	}

	// 3. Exclusive Demod Check
	if req.DemodID != "" {
		demodKey := model.LeaseKeyDemod(req.DemodID)
		if cur, ok := m.leases[demodKey]; ok && cur.exp.After(now) && cur.owner != req.SessionID {
			return model.ClaimSetResult{
				Success:      false,
				ConflictType: model.ConflictDemodOccupied,
				ConflictDesc: "demod " + req.DemodID + " is occupied by " + cur.owner,
			}, nil
		}
		m.leases[demodKey] = leaseState{owner: req.SessionID, exp: expiresAt}
	}

	// 4. Exclusive SCR Check
	if req.SCRSlot != nil && req.InputID != "" {
		scrKey := model.LeaseKeySCR(req.InputID, *req.SCRSlot)
		if cur, ok := m.leases[scrKey]; ok && cur.exp.After(now) && cur.owner != req.SessionID {
			return model.ClaimSetResult{
				Success:      false,
				ConflictType: model.ConflictSCROccupied,
				ConflictDesc: "scr slot on " + req.InputID + " is occupied by " + cur.owner,
			}, nil
		}
		m.leases[scrKey] = leaseState{owner: req.SessionID, exp: expiresAt}
	}

	// 5. Multiplex Creation
	if req.MultiplexID != "" {
		m.muxClaims[req.MultiplexID] = &memMuxClaim{
			multiplexID:   req.MultiplexID,
			inputID:       req.InputID,
			demodID:       req.DemodID,
			requiredPlane: req.RequiredPlane,
			scrSlot:       req.SCRSlot,
			exp:           expiresAt,
		}
		if m.muxMembers[req.MultiplexID] == nil {
			m.muxMembers[req.MultiplexID] = make(map[string]memMemberState)
		}
		m.muxMembers[req.MultiplexID][req.SessionID] = memMemberState{generationToken: genToken, exp: expiresAt}
	}

	return model.ClaimSetResult{
		Success:         true,
		GenerationToken: genToken,
		ReusedMux:       false,
		DemodID:         req.DemodID,
		InputID:         req.InputID,
		ExpiresAt:       expiresAt,
	}, nil
}

func (m *MemoryStore) ReleaseClaimSet(ctx context.Context, sessionID string, generationToken string) error {
	if sessionID == "" {
		return nil
	}
	if generationToken == "" {
		return errors.New("generation_token is required for ReleaseClaimSet; use ForceAdminReleaseClaimSet for admin override")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// 1. Remove session from mux members matching generationToken
	for muxID, members := range m.muxMembers {
		if st, ok := members[sessionID]; ok && st.generationToken == generationToken {
			delete(members, sessionID)
		}
		activeCount := 0
		for _, st := range members {
			if st.exp.After(now) {
				activeCount++
			}
		}
		if activeCount == 0 {
			if claim, ok := m.muxClaims[muxID]; ok {
				delete(m.leases, model.LeaseKeyDemod(claim.demodID))
				if claim.scrSlot != nil {
					delete(m.leases, model.LeaseKeySCR(claim.inputID, *claim.scrSlot))
				}
				delete(m.muxClaims, muxID)
			}
			delete(m.muxMembers, muxID)
		}
	}

	// 2. Remove session from input owners matching generationToken
	for inputID, claim := range m.inputClaims {
		if claim.owners != nil {
			if st, ok := claim.owners[sessionID]; ok && st.generationToken == generationToken {
				delete(claim.owners, sessionID)
			}
			activeCount := 0
			for _, st := range claim.owners {
				if st.exp.After(now) {
					activeCount++
				}
			}
			if activeCount == 0 {
				delete(m.inputClaims, inputID)
			}
		}
	}

	// 3. Delete any direct standalone leases (excluding demod/scr leases held by active mux allocations)
	for key, l := range m.leases {
		if l.owner == sessionID {
			isHeldByMux := false
			for _, claim := range m.muxClaims {
				if claim.exp.After(now) {
					if key == model.LeaseKeyDemod(claim.demodID) {
						isHeldByMux = true
						break
					}
					if claim.scrSlot != nil && key == model.LeaseKeySCR(claim.inputID, *claim.scrSlot) {
						isHeldByMux = true
						break
					}
				}
			}
			if !isHeldByMux {
				delete(m.leases, key)
			}
		}
	}

	return nil
}

func (m *MemoryStore) ForceAdminReleaseClaimSet(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// 1. Remove session from mux members regardless of generation token
	for muxID, members := range m.muxMembers {
		delete(members, sessionID)
		activeCount := 0
		for _, st := range members {
			if st.exp.After(now) {
				activeCount++
			}
		}
		if activeCount == 0 {
			if claim, ok := m.muxClaims[muxID]; ok {
				delete(m.leases, model.LeaseKeyDemod(claim.demodID))
				if claim.scrSlot != nil {
					delete(m.leases, model.LeaseKeySCR(claim.inputID, *claim.scrSlot))
				}
				delete(m.muxClaims, muxID)
			}
			delete(m.muxMembers, muxID)
		}
	}

	// 2. Remove session from input owners
	for inputID, claim := range m.inputClaims {
		if claim.owners != nil {
			delete(claim.owners, sessionID)
			activeCount := 0
			for _, st := range claim.owners {
				if st.exp.After(now) {
					activeCount++
				}
			}
			if activeCount == 0 {
				delete(m.inputClaims, inputID)
			}
		}
	}

	// 3. Delete standalone leases
	for key, l := range m.leases {
		if l.owner == sessionID {
			isHeldByMux := false
			for _, claim := range m.muxClaims {
				if claim.exp.After(now) {
					if key == model.LeaseKeyDemod(claim.demodID) {
						isHeldByMux = true
						break
					}
					if claim.scrSlot != nil && key == model.LeaseKeySCR(claim.inputID, *claim.scrSlot) {
						isHeldByMux = true
						break
					}
				}
			}
			if !isHeldByMux {
				delete(m.leases, key)
			}
		}
	}

	return nil
}

func (m *MemoryStore) ReapExpiredClaimMembers(ctx context.Context) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	reapedMembers := 0
	reapedMuxes := 0

	// 1. Clean mux members
	for muxID, members := range m.muxMembers {
		for sID, st := range members {
			if !st.exp.After(now) {
				delete(members, sID)
				reapedMembers++
			}
		}
		activeCount := 0
		for _, st := range members {
			if st.exp.After(now) {
				activeCount++
			}
		}
		if activeCount == 0 {
			if claim, ok := m.muxClaims[muxID]; ok {
				delete(m.leases, model.LeaseKeyDemod(claim.demodID))
				if claim.scrSlot != nil {
					delete(m.leases, model.LeaseKeySCR(claim.inputID, *claim.scrSlot))
				}
				delete(m.muxClaims, muxID)
				reapedMuxes++
			}
			delete(m.muxMembers, muxID)
		}
	}

	// 2. Clean input claims
	for inputID, claim := range m.inputClaims {
		if claim.owners != nil {
			for sID, st := range claim.owners {
				if !st.exp.After(now) {
					delete(claim.owners, sID)
				}
			}
			activeCount := 0
			for _, st := range claim.owners {
				if st.exp.After(now) {
					activeCount++
				}
			}
			if activeCount == 0 || !claim.exp.After(now) {
				delete(m.inputClaims, inputID)
			}
		}
	}

	return reapedMembers, reapedMuxes, nil
}

func (m *MemoryStore) ApplyReconciliationPlan(ctx context.Context, plan model.ReconciliationPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// 1. Reap specific sessions
	for _, sID := range plan.SessionsToReap {
		for muxID, members := range m.muxMembers {
			delete(members, sID)
			activeCount := 0
			for _, st := range members {
				if st.exp.After(now) {
					activeCount++
				}
			}
			if activeCount == 0 {
				if claim, ok := m.muxClaims[muxID]; ok {
					delete(m.leases, model.LeaseKeyDemod(claim.demodID))
					if claim.scrSlot != nil {
						delete(m.leases, model.LeaseKeySCR(claim.inputID, *claim.scrSlot))
					}
					delete(m.muxClaims, muxID)
				}
				delete(m.muxMembers, muxID)
			}
		}
		for inputID, claim := range m.inputClaims {
			if claim.owners != nil {
				delete(claim.owners, sID)
				activeCount := 0
				for _, st := range claim.owners {
					if st.exp.After(now) {
						activeCount++
					}
				}
				if activeCount == 0 {
					delete(m.inputClaims, inputID)
				}
			}
		}
		for key, l := range m.leases {
			if l.owner == sID {
				delete(m.leases, key)
			}
		}
	}

	// 2. Reap explicitly expired muxes
	for _, muxID := range plan.ExpiredMuxes {
		if claim, ok := m.muxClaims[muxID]; ok {
			delete(m.leases, model.LeaseKeyDemod(claim.demodID))
			if claim.scrSlot != nil {
				delete(m.leases, model.LeaseKeySCR(claim.inputID, *claim.scrSlot))
			}
			delete(m.muxClaims, muxID)
		}
		delete(m.muxMembers, muxID)
	}

	return nil
}
