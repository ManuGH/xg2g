// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package policy

import (
	"sync"
	"time"

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

type TicketStatus string

const (
	TicketStatusIssued   TicketStatus = "issued"
	TicketStatusConsumed TicketStatus = "consumed"
	TicketStatusReleased TicketStatus = "released"
)

type AdmissionTicket struct {
	TicketID           string               `json:"ticketId"`
	HouseholdID        string               `json:"householdId"`
	UserID             string               `json:"userId"`
	ProfileID          string               `json:"profileId"`
	SessionID          string               `json:"sessionId"`
	ServiceRef         string               `json:"serviceRef"`
	RequestType        AdmissionRequestType `json:"requestType"`
	IssuedAt           time.Time            `json:"issuedAt"`
	Status             TicketStatus         `json:"status"`
	DisplacedSessionID string               `json:"displacedSessionId,omitempty"`
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
	CreatedAt    time.Time
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

// IssueAdmissionTicket performs pre-allocation resource admission and issues a mandatory AdmissionTicket.
func (a *HouseholdResourceAdmission) IssueAdmissionTicket(req AdmissionRequest, policy *identity.HouseholdResourcePolicy) (*AdmissionTicket, AdmissionDecision) {
	decision := a.EvaluateAndReserve(req, policy)
	if !decision.Allowed {
		return nil, decision
	}

	ticket := &AdmissionTicket{
		TicketID:           "tkt_" + req.SessionID,
		HouseholdID:        "default_household",
		UserID:             req.UserID,
		ProfileID:          req.ProfileID,
		SessionID:          req.SessionID,
		ServiceRef:         req.ServiceRef,
		RequestType:        req.RequestType,
		IssuedAt:           time.Now().UTC(),
		Status:             TicketStatusIssued,
		DisplacedSessionID: decision.DisplacedSessionID,
	}

	return ticket, decision
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
			displacedID := a.findPreemptionCandidateLocked(req, policy)
			if displacedID != "" {
				delete(a.sessions, displacedID)
				a.sessions[req.SessionID] = SessionRegistration{
					SessionID:    req.SessionID,
					UserID:       req.UserID,
					Role:         req.Role,
					ProfileID:    req.ProfileID,
					ServiceRef:   req.ServiceRef,
					RequestType:  req.RequestType,
					IsTranscoded: req.IsTranscoded,
					CreatedAt:    time.Now().UTC(),
				}
				return AdmissionDecision{
					Allowed:            true,
					DisplacedSessionID: displacedID,
					StatusCode:         200,
				}
			}
		}
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "Maximum concurrent household viewers limit reached",
			StatusCode: 429,
		}
	}

	// 2. Check recording limit
	if req.RequestType == AdmissionRequestRecord && activeRecordings >= policy.MaxParallelRecordings {
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "Maximum parallel recording slots reached",
			StatusCode: 429,
		}
	}

	// 3. Check live services limit
	if req.ServiceRef != "" && !activeLiveServices[req.ServiceRef] && len(activeLiveServices) >= policy.MaxConcurrentLiveServices {
		if policy.PreemptionEnabled {
			displacedID := a.findPreemptionCandidateLocked(req, policy)
			if displacedID != "" {
				delete(a.sessions, displacedID)
				a.sessions[req.SessionID] = SessionRegistration{
					SessionID:    req.SessionID,
					UserID:       req.UserID,
					Role:         req.Role,
					ProfileID:    req.ProfileID,
					ServiceRef:   req.ServiceRef,
					RequestType:  req.RequestType,
					IsTranscoded: req.IsTranscoded,
					CreatedAt:    time.Now().UTC(),
				}
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
		a.sessions[req.SessionID] = SessionRegistration{
			SessionID:    req.SessionID,
			UserID:       req.UserID,
			Role:         req.Role,
			ProfileID:    req.ProfileID,
			ServiceRef:   req.ServiceRef,
			RequestType:  req.RequestType,
			IsTranscoded: req.IsTranscoded,
			CreatedAt:    time.Now().UTC(),
		}
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

func (a *HouseholdResourceAdmission) findPreemptionCandidateLocked(req AdmissionRequest, policy *identity.HouseholdResourcePolicy) string {
	ranks := policy.PreemptionPriorityRanks
	if len(ranks) == 0 {
		ranks = []string{"scheduled_recording", "manual_recording", "admin_live", "member_live", "guest_live"}
	}

	requesterClass := getResourceClass(req)
	requesterPriority := getPriorityRank(requesterClass, ranks)

	var lowestPriority int = requesterPriority
	var candidateID string
	var candidateCreatedAt time.Time

	for id, s := range a.sessions {
		// Rule 5: Recording Protection -> Scheduled recordings never preempt another recording unless policy permits
		if s.RequestType == AdmissionRequestRecord && req.RequestType == AdmissionRequestRecord {
			continue
		}

		sClass := getResourceClass(AdmissionRequest{
			SessionID:    s.SessionID,
			UserID:       s.UserID,
			Role:         s.Role,
			ProfileID:    s.ProfileID,
			ServiceRef:   s.ServiceRef,
			RequestType:  s.RequestType,
			IsTranscoded: s.IsTranscoded,
		})
		pRank := getPriorityRank(sClass, ranks)

		// Candidate must have lower priority (higher rank index)
		if pRank > requesterPriority {
			// Deterministic Tie-Breaker Rule: Select lowest priority rank first; if tied, select youngest session (CreatedAt is newer).
			if pRank > lowestPriority || (pRank == lowestPriority && (candidateID == "" || s.CreatedAt.After(candidateCreatedAt))) {
				lowestPriority = pRank
				candidateID = id
				candidateCreatedAt = s.CreatedAt
			}
		}
	}
	return candidateID
}

func getResourceClass(req AdmissionRequest) string {
	if req.RequestType == AdmissionRequestRecord {
		return "scheduled_recording"
	}
	switch req.Role {
	case identity.RoleAdmin:
		return "admin_live"
	case identity.RoleMember:
		return "member_live"
	default:
		return "guest_live"
	}
}

func getPriorityRank(class string, ranks []string) int {
	for idx, r := range ranks {
		if r == class {
			return idx
		}
	}
	return len(ranks)
}
