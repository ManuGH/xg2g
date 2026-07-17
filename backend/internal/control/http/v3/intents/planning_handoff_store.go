package intents

import (
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playbackreceipt"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/normalize"
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

func (s *PlanningHandoffStore) IssuePlanned(req PlanningHandoffIssue) (PlanningHandoff, error) {
	if req.Result.Plan.Decision != playbackplanner.DecisionAllow || req.Result.Plan.Outcome != playbackplanner.DecisionAllow {
		return PlanningHandoff{}, fmt.Errorf("planner receipt cannot authorize decision %q outcome %q: %w", req.Result.Plan.Decision, req.Result.Plan.Outcome, ErrInvalidPlanningHandoff)
	}
	if req.Result.Trace.PlannerVersion != playbackplanner.PlannerVersion {
		return PlanningHandoff{}, fmt.Errorf("planner version mismatch %q vs %q: %w", req.Result.Trace.PlannerVersion, playbackplanner.PlannerVersion, ErrPlanningVersionMismatch)
	}
	evHash, err := req.Evidence.Hash()
	if err != nil {
		return PlanningHandoff{}, fmt.Errorf("hash evidence: %w", err)
	}
	if evHash != req.Result.Trace.EvidenceHash {
		return PlanningHandoff{}, fmt.Errorf("evidence hash mismatch: %w", ErrPlanningHashMismatch)
	}
	if req.PolicyVersion != req.Evidence.PolicyVersion || req.PolicyVersion != req.Result.Trace.PolicyVersion {
		return PlanningHandoff{}, fmt.Errorf("policy version mismatch across issue (%q), evidence (%q), and trace (%q): %w", req.PolicyVersion, req.Evidence.PolicyVersion, req.Result.Trace.PolicyVersion, ErrPlanningVersionMismatch)
	}
	if normalize.ServiceRef(req.Evidence.SourceIdentity) != normalize.ServiceRef(req.ServiceRef) {
		return PlanningHandoff{}, fmt.Errorf("service ref mismatch %q vs %q: %w", req.Evidence.SourceIdentity, req.ServiceRef, ErrPlanningBindingMismatch)
	}
	if req.Evidence.Scope != req.Scope {
		return PlanningHandoff{}, fmt.Errorf("scope mismatch %q vs %q: %w", req.Evidence.Scope, req.Scope, ErrPlanningBindingMismatch)
	}
	if err := validatePlannerExecutionPlan(req.Result.Plan); err != nil {
		return PlanningHandoff{}, fmt.Errorf("invalid execution plan: %w", err)
	}
	return s.issue(req)
}

func (s *PlanningHandoffStore) issue(req PlanningHandoffIssue) (PlanningHandoff, error) {
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
