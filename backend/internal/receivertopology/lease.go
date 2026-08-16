// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Priority defines precedence ranking between competing streams, recordings, and previews.
type Priority int

const (
	// PriorityPreview represents background channel previews or EPG metadata probing.
	PriorityPreview Priority = 10

	// PriorityPiP represents secondary Picture-in-Picture streams.
	PriorityPiP Priority = 20

	// PriorityLive represents active primary live playback by a user.
	PriorityLive Priority = 50

	// PriorityUpcomingRecording represents a scheduled timer recording within its reservation window.
	PriorityUpcomingRecording Priority = 80

	// PriorityActiveRecording represents an ongoing DVR recording (highest priority).
	PriorityActiveRecording Priority = 100
)

func (p Priority) String() string {
	switch p {
	case PriorityPreview:
		return "PREVIEW(10)"
	case PriorityPiP:
		return "PIP(20)"
	case PriorityLive:
		return "LIVE(50)"
	case PriorityUpcomingRecording:
		return "UPCOMING_RECORDING(80)"
	case PriorityActiveRecording:
		return "ACTIVE_RECORDING(100)"
	default:
		return fmt.Sprintf("PRIORITY(%d)", p)
	}
}

// Lease represents an atomic, time-bounded hardware/multiplex reservation for a stream.
type Lease struct {
	LeaseID     string          `json:"leaseId"`
	SessionID   string          `json:"sessionId"`
	MultiplexID MultiplexID     `json:"multiplexId"`
	DemodID     DemodulatorID   `json:"demodId"`
	InputID     InputID         `json:"inputId"`
	Priority    Priority        `json:"priority"`
	Owner       AllocationOwner `json:"owner"`
	ExpiresAt   time.Time       `json:"expiresAt"`
	ReusedDemod bool            `json:"reusedDemod"`
}

// IsExpired checks if the lease duration has elapsed.
func (l Lease) IsExpired(now time.Time) bool {
	return now.After(l.ExpiresAt)
}

// LeaseStore manages atomic reservation leases with heartbeats and sweepers.
type LeaseStore struct {
	mu     sync.RWMutex
	leases map[string]*Lease // Keyed by sessionID
}

// NewLeaseStore creates an empty lease store.
func NewLeaseStore() *LeaseStore {
	return &LeaseStore{
		leases: make(map[string]*Lease),
	}
}

// GenerateLeaseID creates a cryptographically random 128-bit hex lease identifier.
func GenerateLeaseID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Put records a lease for a session.
func (s *LeaseStore) Put(lease *Lease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leases[lease.SessionID] = lease
}

// Get retrieves an active lease by session ID.
func (s *LeaseStore) Get(sessionID string) (*Lease, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lease, ok := s.leases[sessionID]
	if !ok || lease.IsExpired(time.Now().UTC()) {
		return nil, false
	}
	return lease, true
}

// Heartbeat extends the expiration time of an active lease.
func (s *LeaseStore) Heartbeat(sessionID string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[sessionID]
	if !ok {
		return false
	}
	lease.ExpiresAt = time.Now().UTC().Add(ttl)
	return true
}

// Remove drops a lease on explicit stream teardown.
func (s *LeaseStore) Remove(sessionID string) (*Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[sessionID]
	if ok {
		delete(s.leases, sessionID)
		return lease, true
	}
	return nil, false
}

// SweepExpired removes all expired leases and returns their session IDs.
func (s *LeaseStore) SweepExpired(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []string
	for sessionID, lease := range s.leases {
		if lease.IsExpired(now) {
			expired = append(expired, sessionID)
			delete(s.leases, sessionID)
		}
	}
	return expired
}
