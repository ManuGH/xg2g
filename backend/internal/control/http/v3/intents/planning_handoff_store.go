package intents

import (
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playbackreceipt"
	"github.com/ManuGH/xg2g/internal/control/playbackshadow"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
)

var (
	ErrInvalidPlanningHandoff  = playbackreceipt.ErrInvalidReceipt
	ErrPlanningHandoffMissing  = playbackreceipt.ErrReceiptNotFound
	ErrPlanningHandoffExpired  = playbackreceipt.ErrReceiptExpired
	ErrPlanningBindingMismatch = playbackreceipt.ErrBindingMismatch
	ErrPlanningHashMismatch    = playbackreceipt.ErrHashMismatch
	ErrPlanningVersionMismatch = playbackreceipt.ErrVersionMismatch
	ErrPlanningAlreadyConsumed = playbackreceipt.ErrAlreadyConsumed
)

// PlanningHandoff is the immutable planner result transferred from playback
// discovery to intent creation. Lifecycle metadata is updated only by Store.
type PlanningHandoff struct {
	Receipt  playbackplanner.PlanningReceipt
	Evidence playbackplanner.PlaybackEvidence
	Plan     playbackplanner.PlaybackPlan
	Trace    playbackplanner.PlanTrace
}

type PlanningHandoffIssue struct {
	Evidence      playbackplanner.PlaybackEvidence
	Result        playbackplanner.PlanningResult
	PrincipalID   string
	ServiceRef    string
	Scope         string
	PolicyVersion string
}

type PlanningHandoffBinding struct {
	ReceiptID      string
	EvidenceHash   string
	PlanHash       string
	PlannerVersion string
	PolicyVersion  string
	PrincipalID    string
	ServiceRef     string
	Scope          string
}

type PlanningHandoffStoreConfig struct {
	Capacity int
	TTL      time.Duration
	Clock    func() time.Time
	NewID    func() string
}

// PlanningHandoffStore owns the short-lived transfer between the playback-info
// producer and the intent consumer. The HTTP composition root depends on this
// intent-layer port instead of the concrete receipt persistence adapter.
type PlanningHandoffStore struct {
	store *playbackreceipt.Store
}

func NewPlanningHandoffStore(cfg PlanningHandoffStoreConfig) *PlanningHandoffStore {
	return &PlanningHandoffStore{store: playbackreceipt.NewStore(playbackreceipt.Config{
		Capacity: cfg.Capacity,
		TTL:      cfg.TTL,
		Clock:    cfg.Clock,
		NewID:    cfg.NewID,
	})}
}

func (s *PlanningHandoffStore) Issue(req PlanningHandoffIssue) (PlanningHandoff, error) {
	record, err := s.store.Issue(playbackreceipt.IssueRequest{
		Evidence:      req.Evidence,
		Result:        req.Result,
		PrincipalID:   req.PrincipalID,
		ServiceRef:    req.ServiceRef,
		Scope:         req.Scope,
		PolicyVersion: req.PolicyVersion,
	})
	return planningHandoffFromRecord(record), err
}

// IssueEquivalent evaluates the immutable evidence and issues a handoff only
// when the planner agrees with the fully resolved legacy decision.
func (s *PlanningHandoffStore) IssueEquivalent(evidence playbackplanner.PlaybackEvidence, legacyDecision *decision.Decision, principalID, serviceRef, scope string) (PlanningHandoff, error) {
	result, err := playbackplanner.Plan(evidence)
	if err != nil {
		return PlanningHandoff{}, fmt.Errorf("plan playback evidence: %w", err)
	}
	legacy := playbackshadow.ComparableFromLegacy(legacyDecision)
	planned := playbackshadow.ComparableFromPlanner(result.Plan)
	if blockers := playbackshadow.UnexplainedDiffCodesWithEvidence(legacy, planned, evidence); len(blockers) > 0 {
		return PlanningHandoff{}, fmt.Errorf("planner receipt blocked by unexplained diffs: %v", blockers)
	}
	if result.Plan.Decision != playbackplanner.DecisionAllow {
		return PlanningHandoff{}, fmt.Errorf("planner receipt cannot authorize outcome %q", result.Plan.Decision)
	}
	return s.Issue(PlanningHandoffIssue{
		Evidence:      evidence,
		Result:        result,
		PrincipalID:   principalID,
		ServiceRef:    serviceRef,
		Scope:         scope,
		PolicyVersion: result.Trace.PolicyVersion,
	})
}

func (s *PlanningHandoffStore) Resolve(binding PlanningHandoffBinding) (PlanningHandoff, error) {
	record, err := s.store.Resolve(playbackreceipt.Binding{
		ReceiptID:      binding.ReceiptID,
		EvidenceHash:   binding.EvidenceHash,
		PlanHash:       binding.PlanHash,
		PlannerVersion: binding.PlannerVersion,
		PolicyVersion:  binding.PolicyVersion,
		PrincipalID:    binding.PrincipalID,
		ServiceRef:     binding.ServiceRef,
		Scope:          binding.Scope,
	})
	return planningHandoffFromRecord(record), err
}

func (s *PlanningHandoffStore) Consume(receiptID, sessionID string) (PlanningHandoff, error) {
	record, err := s.store.Consume(receiptID, sessionID)
	return planningHandoffFromRecord(record), err
}

func (s *PlanningHandoffStore) Len() int {
	if s == nil {
		return 0
	}
	return s.store.Len()
}

// ApplyTo binds the exact stored planner values to intent processing without
// exposing planner-domain transport details to the HTTP composition root.
func (h *PlanningHandoff) ApplyTo(intent *Intent) {
	if h == nil || intent == nil {
		return
	}
	plan := h.Plan
	receipt := h.Receipt
	evidence := h.Evidence
	intent.PlannerPlan = &plan
	intent.PlanningReceipt = &receipt
	intent.PlannerEvidence = &evidence
}

func planningHandoffFromRecord(record playbackreceipt.Record) PlanningHandoff {
	return PlanningHandoff{
		Receipt:  record.Receipt,
		Evidence: record.Evidence,
		Plan:     record.Plan,
		Trace:    record.Trace,
	}
}
