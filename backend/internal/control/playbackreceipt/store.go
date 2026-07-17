package playbackreceipt

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
)

const (
	StateIssued   = "issued"
	StateConsumed = "consumed"
	StateExpired  = "expired"

	defaultCapacity = 1024
	defaultTTL      = 60 * time.Second
)

var (
	ErrInvalidReceipt  = errors.New("invalid planning receipt")
	ErrReceiptNotFound = errors.New("planning receipt not found")
	ErrReceiptExpired  = errors.New("planning receipt expired")
	ErrBindingMismatch = errors.New("planning receipt binding mismatch")
	ErrHashMismatch    = errors.New("planning receipt hash mismatch")
	ErrVersionMismatch = errors.New("planning receipt version mismatch")
	ErrAlreadyConsumed = errors.New("planning receipt already consumed by another session")
)

// Record is the immutable planner result stored behind a short-lived receipt.
// The Receipt lifecycle metadata is the only mutable part.
type Record struct {
	Receipt  playbackplanner.PlanningReceipt
	Evidence playbackplanner.PlaybackEvidence
	Plan     playbackplanner.PlaybackPlan
	Trace    playbackplanner.PlanTrace
}

// IssueRequest contains the complete binding context for a receipt. All values
// are copied by Store; callers may safely reuse their input afterwards.
type IssueRequest struct {
	Evidence      playbackplanner.PlaybackEvidence
	Result        playbackplanner.PlanningResult
	PrincipalID   string
	ServiceRef    string
	Scope         string
	PolicyVersion string
}

// Binding is reconstructed from signed token claims and the current request.
// Resolve requires every field to match the stored receipt.
type Binding struct {
	ReceiptID      string
	EvidenceHash   string
	PlanHash       string
	PlannerVersion string
	PolicyVersion  string
	PrincipalID    string
	ServiceRef     string
	Scope          string
}

type Config struct {
	Capacity int
	TTL      time.Duration
	Clock    func() time.Time
	NewID    func() string
}

// Store is an in-memory, bounded receipt store. Receipts intentionally do not
// survive a process restart; a missing receipt forces an explicit preview
// refresh instead of silently re-planning with potentially changed evidence.
type Store struct {
	mu       sync.Mutex
	records  map[string]Record
	capacity int
	ttl      time.Duration
	clock    func() time.Time
	newID    func() string
}

func NewStore(cfg Config) *Store {
	if cfg.Capacity <= 0 {
		cfg.Capacity = defaultCapacity
	}
	if cfg.TTL <= 0 {
		cfg.TTL = defaultTTL
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = func() string { return uuid.NewString() }
	}
	return &Store{
		records:  make(map[string]Record, cfg.Capacity),
		capacity: cfg.Capacity,
		ttl:      cfg.TTL,
		clock:    cfg.Clock,
		newID:    cfg.NewID,
	}
}

func (s *Store) Issue(req IssueRequest) (Record, error) {
	if s == nil || req.ServiceRef == "" || req.Scope == "" {
		return Record{}, ErrInvalidReceipt
	}
	evidenceHash, err := req.Evidence.Hash()
	if err != nil {
		return Record{}, fmt.Errorf("hash playback evidence: %w", err)
	}
	planHash, err := req.Result.Plan.Hash()
	if err != nil {
		return Record{}, fmt.Errorf("hash playback plan: %w", err)
	}
	plannerVersion := req.Result.Trace.PlannerVersion
	if plannerVersion == "" {
		return Record{}, fmt.Errorf("%w: planner version is empty", ErrInvalidReceipt)
	}
	policyVersion := req.PolicyVersion
	if policyVersion == "" {
		policyVersion = req.Evidence.PolicyVersion
	}
	if policyVersion == "" {
		policyVersion = "unknown"
	}

	now := s.clock().UTC()
	record := cloneRecord(Record{
		Receipt: playbackplanner.PlanningReceipt{
			ReceiptID:      s.newID(),
			EvidenceHash:   evidenceHash,
			PlanHash:       planHash,
			IssuedAt:       now.UnixMilli(),
			ExpiresAt:      now.Add(s.ttl).UnixMilli(),
			PlannerVersion: plannerVersion,
			PolicyVersion:  policyVersion,
			PrincipalBind:  req.PrincipalID,
			ServiceRefBind: req.ServiceRef,
			ScopeBind:      req.Scope,
			LifecycleState: StateIssued,
		},
		Evidence: req.Evidence,
		Plan:     req.Result.Plan,
		Trace:    req.Result.Trace,
	})
	if record.Receipt.ReceiptID == "" {
		return Record{}, fmt.Errorf("%w: receipt id is empty", ErrInvalidReceipt)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now.UnixMilli())
	if len(s.records) >= s.capacity {
		s.evictOldestLocked()
	}
	s.records[record.Receipt.ReceiptID] = record
	return cloneRecord(record), nil
}

func (s *Store) Resolve(binding Binding) (Record, error) {
	if s == nil || binding.ReceiptID == "" {
		return Record{}, ErrReceiptNotFound
	}
	nowMS := s.clock().UTC().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[binding.ReceiptID]
	if !ok {
		return Record{}, ErrReceiptNotFound
	}
	if record.Receipt.ExpiresAt <= nowMS {
		record.Receipt.LifecycleState = StateExpired
		delete(s.records, binding.ReceiptID)
		return Record{}, ErrReceiptExpired
	}
	if record.Receipt.PrincipalBind != binding.PrincipalID ||
		record.Receipt.ServiceRefBind != binding.ServiceRef ||
		record.Receipt.ScopeBind != binding.Scope {
		return Record{}, ErrBindingMismatch
	}
	if !secureEqual(record.Receipt.EvidenceHash, binding.EvidenceHash) ||
		!secureEqual(record.Receipt.PlanHash, binding.PlanHash) {
		return Record{}, ErrHashMismatch
	}
	if record.Receipt.PlannerVersion != binding.PlannerVersion ||
		record.Receipt.PolicyVersion != binding.PolicyVersion {
		return Record{}, ErrVersionMismatch
	}
	return cloneRecord(record), nil
}

// Consume marks a receipt as consumed by sessionID. Repeating the operation for
// the same session is idempotent; a different session is rejected.
func (s *Store) Consume(receiptID, sessionID string) (Record, error) {
	if s == nil || receiptID == "" || sessionID == "" {
		return Record{}, ErrInvalidReceipt
	}
	nowMS := s.clock().UTC().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[receiptID]
	if !ok {
		return Record{}, ErrReceiptNotFound
	}
	if record.Receipt.ExpiresAt <= nowMS {
		delete(s.records, receiptID)
		return Record{}, ErrReceiptExpired
	}
	if consumed := record.Receipt.ConsumedSessionID; consumed != "" && consumed != sessionID {
		return Record{}, ErrAlreadyConsumed
	}
	record.Receipt.LifecycleState = StateConsumed
	record.Receipt.ConsumedSessionID = sessionID
	s.records[receiptID] = record
	return cloneRecord(record), nil
}

func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(s.clock().UTC().UnixMilli())
	return len(s.records)
}

func (s *Store) pruneExpiredLocked(nowMS int64) {
	for id, record := range s.records {
		if record.Receipt.ExpiresAt <= nowMS {
			delete(s.records, id)
		}
	}
}

func (s *Store) evictOldestLocked() {
	var oldestID string
	var oldestIssuedAt int64
	for id, record := range s.records {
		if oldestID == "" || record.Receipt.IssuedAt < oldestIssuedAt ||
			(record.Receipt.IssuedAt == oldestIssuedAt && id < oldestID) {
			oldestID = id
			oldestIssuedAt = record.Receipt.IssuedAt
		}
	}
	if oldestID != "" {
		delete(s.records, oldestID)
	}
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return hmac.Equal([]byte(left), []byte(right))
}

func cloneRecord(in Record) Record {
	out := in
	out.Evidence.ClientEvidence.SupportedContainers = append([]string(nil), in.Evidence.ClientEvidence.SupportedContainers...)
	out.Evidence.ClientEvidence.SupportedVideoCodecs = append([]string(nil), in.Evidence.ClientEvidence.SupportedVideoCodecs...)
	out.Evidence.ClientEvidence.SupportedAudioCodecs = append([]string(nil), in.Evidence.ClientEvidence.SupportedAudioCodecs...)
	out.Evidence.ClientEvidence.AutoTranscodeVideoCodecs = append([]string(nil), in.Evidence.ClientEvidence.AutoTranscodeVideoCodecs...)
	out.Evidence.ClientEvidence.SupportedEngines = append([]string(nil), in.Evidence.ClientEvidence.SupportedEngines...)
	if in.Evidence.ClientEvidence.SupportsRange != nil {
		supportsRange := *in.Evidence.ClientEvidence.SupportsRange
		out.Evidence.ClientEvidence.SupportsRange = &supportsRange
	}
	out.Evidence.HostSnapshot.AvailableEngines = append([]string(nil), in.Evidence.HostSnapshot.AvailableEngines...)
	out.Evidence.HostSnapshot.EncoderCapabilities = append([]playbackplanner.HostEncoderCapability(nil), in.Evidence.HostSnapshot.EncoderCapabilities...)
	out.Plan.Guardrails.PermittedAlternativePlans = append([]string(nil), in.Plan.Guardrails.PermittedAlternativePlans...)
	out.Trace.Log = append([]playbackplanner.RuleHit(nil), in.Trace.Log...)
	return out
}
