// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"fmt"
	"sync"
	"time"
)

// ActiveSessionInfo provides basic information about an active stream session for topology reconciliation.
type ActiveSessionInfo struct {
	SessionID  string
	ServiceRef string
}

// Service coordinates receiver physical topology evaluation, capacity checking,
// atomic leases, recording reservations, and external receiver activity.
type Service struct {
	mu           sync.RWMutex
	allocator    *Allocator
	runtime      *RuntimeAllocation
	leases       *LeaseStore
	planner      *ReservationPlanner
	lastSnapshot ReceiverRuntimeSnapshot
}

var (
	// ErrInvalidTransition indicates a disallowed topology confidence state transition.
	ErrInvalidTransition = fmt.Errorf("invalid topology confidence state transition")
	// ErrEnforceRequiresVerified indicates that ENFORCE mode was requested for a non-verified topology.
	ErrEnforceRequiresVerified = fmt.Errorf("evaluation mode ENFORCE requires ConfidenceVerified")
)

// NewService initializes a receiver topology service with the given topology and evaluation mode.
// Enforces that ENFORCE mode is strictly permitted ONLY for ConfidenceVerified topologies.
func NewService(topology ReceiverTopology, mode EvaluationMode) (*Service, error) {
	if mode == EvaluationModeEnforce && topology.Confidence != ConfidenceVerified {
		return nil, fmt.Errorf("%w: cannot use ENFORCE with confidence %q", ErrEnforceRequiresVerified, topology.Confidence)
	}

	if topology.Confidence == ConfidenceVerified {
		if err := Validate(topology); err != nil {
			return nil, fmt.Errorf("cannot initialize verified topology: %w", err)
		}
	} else {
		// Non-verified topologies (Observed, Default) are strictly pinned to AUDIT_ONLY mode.
		mode = EvaluationModeAuditOnly
	}

	allocator := NewAllocator(topology, mode)
	return &Service{
		allocator: allocator,
		runtime:   NewRuntimeAllocation(),
		leases:    NewLeaseStore(),
		planner:   NewReservationPlanner(DefaultReservationWindow),
	}, nil
}

// Topology returns the active receiver topology snapshot.
func (s *Service) Topology() ReceiverTopology {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.allocator.Topology()
}

// EvidentiarySnapshot returns the most recently collected evidentiary receiver runtime snapshot.
func (s *Service) EvidentiarySnapshot() ReceiverRuntimeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSnapshot
}

// UpdateEvidentiarySnapshot stores a new evidentiary runtime observation snapshot.
func (s *Service) UpdateEvidentiarySnapshot(snap ReceiverRuntimeSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSnapshot = snap
}

// EffectiveTunerCapacity returns the maximum physically independent concurrent tuner capacity of the active topology.
func (s *Service) EffectiveTunerCapacity() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.allocator.Topology().EffectiveTunerCapacity()
}

// CloneRuntime returns a deep copy snapshot of active runtime allocations for inspection.
func (s *Service) CloneRuntime() *RuntimeAllocation {
	return s.runtime.Clone()
}

// Mode returns the active evaluation mode (ENFORCE or AUDIT_ONLY).
func (s *Service) Mode() EvaluationMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.allocator.Mode()
}

// UpdateTopology updates the receiver topology (e.g. after config reload or discovery).
// Enforces that ENFORCE mode is strictly rejected for non-verified topologies.
func (s *Service) UpdateTopology(topology ReceiverTopology, mode EvaluationMode) error {
	if mode == EvaluationModeEnforce && topology.Confidence != ConfidenceVerified {
		return fmt.Errorf("%w: cannot use ENFORCE with confidence %q", ErrEnforceRequiresVerified, topology.Confidence)
	}

	if topology.Confidence == ConfidenceVerified {
		if err := Validate(topology); err != nil {
			return fmt.Errorf("cannot update to verified topology: %w", err)
		}
	} else {
		mode = EvaluationModeAuditOnly
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.allocator = NewAllocator(topology, mode)
	return nil
}

// UpdateTopologyWithPriority updates the active topology following strict confidence transition invariants:
// 1. Default -> Observed: Allowed (mode: strictly AUDIT_ONLY)
// 2. Observed -> Observed: Allowed (mode: strictly AUDIT_ONLY)
// 3. Observed -> Default: Forbidden
// 4. Verified -> Observed / Default: Forbidden (verified config is sticky)
// 5. Any -> Verified: Allowed on explicit reload (mode: ENFORCE unless explicitly set to AUDIT_ONLY)
func (s *Service) UpdateTopologyWithPriority(newTopology ReceiverTopology, explicitReload bool, desiredMode ...EvaluationMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.allocator.Topology()

	// Handle Verified transitions
	if newTopology.Confidence == ConfidenceVerified {
		if !explicitReload && current.Confidence == ConfidenceVerified {
			return nil // No change
		}
		if err := Validate(newTopology); err != nil {
			return fmt.Errorf("cannot update to verified topology: %w", err)
		}
		mode := EvaluationModeEnforce
		if len(desiredMode) > 0 && desiredMode[0] != "" {
			mode = desiredMode[0]
		}
		s.allocator = NewAllocator(newTopology, mode)
		return nil
	}

	// Current is Verified -> reject non-verified updates
	if current.Confidence == ConfidenceVerified {
		return fmt.Errorf("%w: cannot overwrite VERIFIED topology with %s", ErrInvalidTransition, newTopology.Confidence)
	}

	// Observed transitions - MUST strictly be AUDIT_ONLY
	if newTopology.Confidence == ConfidenceObserved {
		if len(desiredMode) > 0 && desiredMode[0] == EvaluationModeEnforce {
			return fmt.Errorf("%w: cannot use ENFORCE with confidence OBSERVED", ErrEnforceRequiresVerified)
		}
		s.allocator = NewAllocator(newTopology, EvaluationModeAuditOnly)
		return nil
	}

	// New is Default
	if current.Confidence == ConfidenceObserved {
		return fmt.Errorf("%w: cannot downgrade OBSERVED topology to DEFAULT", ErrInvalidTransition)
	}

	// Default -> Default - MUST strictly be AUDIT_ONLY
	if len(desiredMode) > 0 && desiredMode[0] == EvaluationModeEnforce {
		return fmt.Errorf("%w: cannot use ENFORCE with confidence DEFAULT", ErrEnforceRequiresVerified)
	}
	s.allocator = NewAllocator(newTopology, EvaluationModeAuditOnly)
	return nil
}

// Planner returns the recording reservation planner.
func (s *Service) Planner() *ReservationPlanner {
	return s.planner
}

// SyncTimers updates the recording reservation planner with upcoming Enigma2 timer recordings.
func (s *Service) SyncTimers(timers []RecordingReservation) {
	s.planner.SyncTimers(timers)
}

// CanStartStream evaluates whether a stream for serviceRef can be allocated without committing.
func (s *Service) CanStartStream(serviceRef, sessionID string) (AllocationDecision, error) {
	return s.CanStartStreamWithPriority(serviceRef, sessionID, PriorityLive)
}

// CanStartStreamWithPriority evaluates whether a stream at a given priority can be allocated,
// taking upcoming recording reservations into account.
func (s *Service) CanStartStreamWithPriority(serviceRef, sessionID string, priority Priority) (AllocationDecision, error) {
	mux, err := ParseServiceRef(serviceRef)
	if err != nil {
		return AllocationDecision{}, fmt.Errorf("cannot parse service ref %q: %w", serviceRef, err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.allocator.EvaluateWithUpcomingReservations(
		s.runtime,
		s.planner,
		mux,
		sessionID,
		priority,
		time.Now().UTC(),
	), nil
}

// RegisterStream commits the hardware/multiplex allocation for a newly starting stream.
func (s *Service) RegisterStream(serviceRef, sessionID string) (AllocationDecision, error) {
	_, decision, err := s.ReserveStreamLeaseAtomic(serviceRef, sessionID, PriorityLive, time.Minute)
	return decision, err
}

// ReserveStreamLeaseAtomic performs an atomic check + allocate + lease commit inside a single lock.
// Completely eliminates race conditions when multiple stream requests arrive simultaneously.
func (s *Service) ReserveStreamLeaseAtomic(
	serviceRef string,
	sessionID string,
	priority Priority,
	ttl time.Duration,
) (*Lease, AllocationDecision, error) {
	mux, err := ParseServiceRef(serviceRef)
	if err != nil {
		return nil, AllocationDecision{}, fmt.Errorf("cannot parse service ref %q: %w", serviceRef, err)
	}

	if ttl <= 0 {
		ttl = 30 * time.Second // Default fallback heartbeat lease
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	decision := s.allocator.EvaluateWithUpcomingReservations(
		s.runtime,
		s.planner,
		mux,
		sessionID,
		priority,
		time.Now().UTC(),
	)

	if !decision.Allowed {
		return nil, decision, fmt.Errorf("allocation rejected: %s", decision.Reason)
	}

	// Commit runtime allocation
	_, err = s.allocator.Allocate(s.runtime, mux, sessionID, AllocationOwnerXG2G)
	if err != nil {
		return nil, decision, fmt.Errorf("runtime allocation failed: %w", err)
	}

	// Commit lease
	lease := &Lease{
		LeaseID:     GenerateLeaseID(),
		SessionID:   sessionID,
		MultiplexID: mux,
		DemodID:     decision.DemodID,
		InputID:     decision.InputID,
		Priority:    priority,
		Owner:       AllocationOwnerXG2G,
		ExpiresAt:   time.Now().UTC().Add(ttl),
		ReusedDemod: decision.ReusedDemod,
	}
	s.leases.Put(lease)

	return lease, decision, nil
}

// HeartbeatStream extends the lease TTL for an active session.
func (s *Service) HeartbeatStream(sessionID string, ttl time.Duration) bool {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return s.leases.Heartbeat(sessionID, ttl)
}

// ReleaseStream frees the demodulator / RF plane and removes the active lease.
func (s *Service) ReleaseStream(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.leases.Remove(sessionID)
	return s.allocator.Release(s.runtime, sessionID)
}

// SweepExpiredLeases cleans up any abandoned leases whose heartbeat stopped.
func (s *Service) SweepExpiredLeases(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := s.leases.SweepExpired(now)
	for _, sessID := range expired {
		s.allocator.Release(s.runtime, sessID)
	}
	return expired
}

// UpdateExternalAllocations updates the observed external tuner activity on the receiver (HDMI live, local DVR, etc.).
func (s *Service) UpdateExternalAllocations(external []ExternalAllocation) {
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	s.runtime.ExternalAllocations = append([]ExternalAllocation(nil), external...)
}

// RuntimeSnapshot returns a deep copy of current active demodulator and plane allocations.
func (s *Service) RuntimeSnapshot() *RuntimeAllocation {
	return s.runtime.Clone()
}

// ActiveDemods returns all demodulators currently occupied by active xg2g stream leases.
func (s *Service) ActiveDemods() map[DemodulatorID]bool {
	return s.leases.ActiveDemods()
}

// ReconcileActiveSessions synchronizes runtime allocations and leases with the authoritative active sessions list.
func (s *Service) ReconcileActiveSessions(active []ActiveSessionInfo) {
	activeSet := make(map[string]bool, len(active))
	for _, a := range active {
		activeSet[a.SessionID] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up abandoned leases thread-safely
	s.leases.RetainActive(activeSet)

	// Clean up runtime allocations
	for key, alloc := range s.runtime.ActiveMultiplexes {
		var surviving []string
		for _, sessID := range alloc.SessionIDs {
			if activeSet[sessID] {
				surviving = append(surviving, sessID)
			}
		}
		alloc.SessionIDs = surviving
		if len(surviving) == 0 {
			inputID := alloc.InputID
			delete(s.runtime.ActiveMultiplexes, key)

			inputStillActive := false
			for _, remaining := range s.runtime.ActiveMultiplexes {
				if remaining.InputID == inputID {
					inputStillActive = true
					break
				}
			}
			if !inputStillActive {
				delete(s.runtime.ActiveInputPlanes, inputID)
			}
		}
	}
}
