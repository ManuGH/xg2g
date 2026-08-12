// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package policy

import (
	"sync"

	"github.com/ManuGH/xg2g/internal/domain/identity"
)

type AdmissionRequestType string

const (
	AdmissionRequestLiveTV          AdmissionRequestType = "live_tv"
	AdmissionRequestRecord          AdmissionRequestType = "record"
	AdmissionRequestWatchRecordings AdmissionRequestType = "watch_recordings"
)

type AdmissionRequest struct {
	SessionID    string               `json:"sessionId"`
	UserID       string               `json:"userId"`
	Role         identity.Role        `json:"role"`
	ProfileID    string               `json:"profileId"`
	ServiceRef   string               `json:"serviceRef"`
	RequestType  AdmissionRequestType `json:"requestType"`
	IsTranscoded bool                 `json:"isTranscoded"`
}

type AdmissionDecision struct {
	Allowed            bool   `json:"allowed"`
	Reason             string `json:"reason,omitempty"`
	DisplacedSessionID string `json:"displacedSessionId,omitempty"`
	StatusCode         int    `json:"statusCode"`
}

// SessionRegistration holds transient state of active viewer/recording streams.
type SessionRegistration struct {
	SessionID    string
	UserID       string
	Role         identity.Role
	ProfileID    string
	ServiceRef   string
	RequestType  AdmissionRequestType
	IsTranscoded bool
}

// HouseholdResourceAdmission manages real-time resource limits and arbitration.
type HouseholdResourceAdmission struct {
	mu       sync.Mutex
	sessions map[string]SessionRegistration
}

func NewHouseholdResourceAdmission() *HouseholdResourceAdmission {
	return &HouseholdResourceAdmission{
		sessions: make(map[string]SessionRegistration),
	}
}

// EvaluateAndReserve performs pre-allocation resource admission.
// If allowed, reserves the session slot atomically.
func (a *HouseholdResourceAdmission) EvaluateAndReserve(req AdmissionRequest, policy *identity.HouseholdResourcePolicy) AdmissionDecision {
	a.mu.Lock()
	defer a.mu.Unlock()

	if policy == nil {
		policy = &identity.HouseholdResourcePolicy{
			MaxConcurrentLiveServices: 3,
			MaxConcurrentViewers:      5,
			MaxParallelRecordings:     4,
			MaxParallelTranscodes:     3,
			PreemptionEnabled:         false,
		}
	}

	activeViewers := 0
	activeRecordings := 0
	activeTranscodes := 0
	activeLiveServices := make(map[string]bool)

	for _, s := range a.sessions {
		if s.RequestType == AdmissionRequestRecord {
			activeRecordings++
		} else {
			activeViewers++
		}
		if s.IsTranscoded {
			activeTranscodes++
		}
		if s.ServiceRef != "" {
			activeLiveServices[s.ServiceRef] = true
		}
	}

	// 1. Check viewer limit
	if req.RequestType != AdmissionRequestRecord && activeViewers >= policy.MaxConcurrentViewers {
		if policy.PreemptionEnabled {
			displacedID := a.findPreemptionCandidateLocked(req.Role)
			if displacedID != "" {
				delete(a.sessions, displacedID)
				a.sessions[req.SessionID] = SessionRegistration(req)
				return AdmissionDecision{
					Allowed:            true,
					DisplacedSessionID: displacedID,
					StatusCode:         200,
				}
			}
		}
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "Maximum concurrent viewers limit reached",
			StatusCode: 429,
		}
	}

	// 2. Check recording limit
	if req.RequestType == AdmissionRequestRecord && activeRecordings >= policy.MaxParallelRecordings {
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "Maximum parallel recordings limit reached",
			StatusCode: 429,
		}
	}

	// 3. Check live service tuner limit (shared tuner exception: tuning existing active service is free)
	if req.ServiceRef != "" && !activeLiveServices[req.ServiceRef] {
		if len(activeLiveServices) >= policy.MaxConcurrentLiveServices {
			if policy.PreemptionEnabled {
				displacedID := a.findPreemptionCandidateLocked(req.Role)
				if displacedID != "" {
					delete(a.sessions, displacedID)
					a.sessions[req.SessionID] = SessionRegistration(req)
					return AdmissionDecision{
						Allowed:            true,
						DisplacedSessionID: displacedID,
						StatusCode:         200,
					}
				}
			}
			return AdmissionDecision{
				Allowed:    false,
				Reason:     "Maximum concurrent live tuner services limit reached",
				StatusCode: 503,
			}
		}
	}

	// 4. Check transcode limit
	if req.IsTranscoded && activeTranscodes >= policy.MaxParallelTranscodes {
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "Maximum parallel transcode slots reached",
			StatusCode: 429,
		}
	}

	// Reserve session
	if req.SessionID != "" {
		a.sessions[req.SessionID] = SessionRegistration(req)
	}

	return AdmissionDecision{
		Allowed:    true,
		StatusCode: 200,
	}
}

// ReleaseSession removes a session upon completion/disconnect.
func (a *HouseholdResourceAdmission) ReleaseSession(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, sessionID)
}

func (a *HouseholdResourceAdmission) findPreemptionCandidateLocked(requesterRole identity.Role) string {
	// Preemption hierarchy: Guest (1) < Member (2) < Admin (3)
	requesterRank := roleRank(requesterRole)
	lowestRank := requesterRank
	candidateID := ""

	for id, s := range a.sessions {
		rank := roleRank(s.Role)
		if rank < lowestRank {
			lowestRank = rank
			candidateID = id
		}
	}
	return candidateID
}

func roleRank(r identity.Role) int {
	switch r {
	case identity.RoleAdmin:
		return 3
	case identity.RoleMember:
		return 2
	case identity.RoleGuest, identity.RoleViewer:
		return 1
	default:
		return 0
	}
}
