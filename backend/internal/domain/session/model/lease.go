package model

import (
	"fmt"
	"math"
	"time"
)

const (
	// NamespaceTunerSlot is the lease namespace for physical tuners
	NamespaceTunerSlot = "tuner"
	// NamespaceService is the lease namespace for service dedup
	NamespaceService = "service"
	// NamespaceDemod is the lease namespace for physical demodulators
	NamespaceDemod = "demod"
	// NamespaceInput is the lease namespace for physical coaxial inputs
	NamespaceInput = "input"
	// NamespaceSCR is the lease namespace for Unicable SCR user bands
	NamespaceSCR = "scr"
	// NamespaceMultiplex is the lease namespace for transport streams
	NamespaceMultiplex = "mux"
)

// ResourceKind represents the category of hardware or virtual resource being claimed.
type ResourceKind string

const (
	ResourceKindDemod     ResourceKind = "demod"
	ResourceKindInput     ResourceKind = "input"
	ResourceKindSCR       ResourceKind = "scr"
	ResourceKindMultiplex ResourceKind = "multiplex"
)

// ConflictKind defines the specific physical or topological reason a claim set cannot be acquired.
type ConflictKind string

const (
	ConflictNone                 ConflictKind = ""
	ConflictDemodOccupied        ConflictKind = "demod_occupied"
	ConflictPlaneConflict        ConflictKind = "plane_conflict"
	ConflictSCROccupied          ConflictKind = "scr_occupied"
	ConflictRecordingReservation ConflictKind = "recording_reservation_conflict"
	ConflictCapacityExhausted    ConflictKind = "capacity_exhausted"
	ConflictDemuxExhausted       ConflictKind = "demux_exhausted"
	ConflictStaleSnapshot        ConflictKind = "stale_snapshot"
)

// DemuxLimitSource denotes the origin of the demux service limit constraint.
type DemuxLimitSource string

const (
	DemuxLimitSourceObserved   DemuxLimitSource = "OBSERVED"
	DemuxLimitSourceConfigured DemuxLimitSource = "CONFIGURED"
	DemuxLimitSourceDefault    DemuxLimitSource = "DEFAULT"
)

// ReconciliationPlan defines a classified set of claims to be transactionally applied by the store.
type ReconciliationPlan struct {
	SessionsToReap []string `json:"sessionsToReap,omitempty"`
	ExpiredMuxes   []string `json:"expiredMuxes,omitempty"`
}

// ClaimSetRequest defines a multi-resource atomic hardware reservation plan.
type ClaimSetRequest struct {
	SessionID       string        `json:"sessionId"`
	GenerationToken string        `json:"generationToken"` // Immutable fencing token (UUIDv4)
	ServiceRef      string        `json:"serviceRef"`
	MultiplexID     string        `json:"multiplexId"`
	InputID         string        `json:"inputId"`
	RequiredPlane   string        `json:"requiredPlane,omitempty"` // e.g. "192:HIGH:H"
	DemodID         string        `json:"demodId"`
	SCRSlot         *int          `json:"scrSlot,omitempty"`
	MaxMuxMembers   int           `json:"maxMuxMembers,omitempty"` // Optional capacity limit per tuned mux (0 = default 8)
	TTL             time.Duration `json:"ttl"`
	Priority        int           `json:"priority"`
}

// ClaimSetResult contains the outcome of an atomic claim acquisition attempt.
type ClaimSetResult struct {
	Success         bool         `json:"success"`
	GenerationToken string       `json:"generationToken,omitempty"`
	ReusedMux       bool         `json:"reusedMux"`
	ConflictType    ConflictKind `json:"conflictType,omitempty"`
	ConflictDesc    string       `json:"conflictDesc,omitempty"`
	DemodID         string       `json:"demodId,omitempty"`
	InputID         string       `json:"inputId,omitempty"`
	ExpiresAt       time.Time    `json:"expiresAt,omitempty"`
}

// MuxAllocationRecord represents an active hardware transport stream allocation.
type MuxAllocationRecord struct {
	MultiplexID   string    `json:"multiplexId"`
	InputID       string    `json:"inputId"`
	DemodID       string    `json:"demodId"`
	RequiredPlane string    `json:"requiredPlane,omitempty"`
	SCRSlot       *int      `json:"scrSlot,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// MuxMemberRecord represents an active session consumer on a shared multiplex.
type MuxMemberRecord struct {
	MultiplexID     string    `json:"multiplexId"`
	SessionID       string    `json:"sessionId"`
	GenerationToken string    `json:"generationToken"`
	JoinedAt        time.Time `json:"joinedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

// InputClaimRecord represents the active RF plane state on a physical coaxial input.
type InputClaimRecord struct {
	InputID       string    `json:"inputId"`
	ActivePlane   string    `json:"activePlane"`
	OwnerSessions []string  `json:"ownerSessions"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// LeaseKeyTunerSlot returns the standard lease key for a given tuner slot.
// e.g. "tuner:0"
func LeaseKeyTunerSlot(slot int) string {
	return fmt.Sprintf("%s:%d", NamespaceTunerSlot, slot)
}

// LeaseKeyService returns the standard lease key for a service reference.
// e.g. "service:1:0:1:..."
func LeaseKeyService(ref string) string {
	return fmt.Sprintf("%s:%s", NamespaceService, ref)
}

// LeaseKeyDemod returns the lease key for an exclusive hardware demodulator.
func LeaseKeyDemod(demodID string) string {
	return fmt.Sprintf("%s:%s", NamespaceDemod, demodID)
}

// LeaseKeySCR returns the lease key for an exclusive Unicable SCR slot.
func LeaseKeySCR(inputID string, slot int) string {
	return fmt.Sprintf("%s:%s:%d", NamespaceSCR, inputID, slot)
}

// LeaseKeyInput returns the lease key for a physical coaxial input.
func LeaseKeyInput(inputID string) string {
	return fmt.Sprintf("%s:%s", NamespaceInput, inputID)
}

// LeaseKeyMultiplex returns the lease key for an active transport stream.
func LeaseKeyMultiplex(muxID string) string {
	return fmt.Sprintf("%s:%s", NamespaceMultiplex, muxID)
}

// SessionInactivityTTL returns the time a session may remain unconsumed before
// it is eligible for lease or idle cleanup.
//
// Live-only sessions retain the short operator-configured base timeout. DVR
// sessions retain their complete rolling window and use that base timeout as a
// resume grace period. This keeps mobile browser suspension from destroying a
// timeshift session before its advertised DVR window has elapsed.
func SessionInactivityTTL(base time.Duration, dvrWindowSec int) time.Duration {
	if base < 0 {
		base = 0
	}
	if dvrWindowSec <= 0 {
		return base
	}

	windowSeconds := int64(dvrWindowSec)
	maxWindowSeconds := int64((time.Duration(math.MaxInt64) - base) / time.Second)
	if windowSeconds > maxWindowSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(windowSeconds)*time.Second + base
}
