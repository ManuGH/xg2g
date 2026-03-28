package scan

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/normalize"
)

type Capability struct {
	ServiceRef    string          `json:"service_ref"`
	Interlaced    bool            `json:"interlaced"`
	LastScan      time.Time       `json:"last_scan"`
	LastAttempt   time.Time       `json:"last_attempt,omitempty"`
	LastSuccess   time.Time       `json:"last_success,omitempty"`
	State         CapabilityState `json:"state,omitempty"`
	FailureReason string          `json:"failure_reason,omitempty"`
	NextRetryAt   time.Time       `json:"next_retry_at,omitempty"`
	Resolution    string          `json:"resolution"`
	Codec         string          `json:"codec"`
	Container     string          `json:"container,omitempty"`
	VideoCodec    string          `json:"video_codec,omitempty"`
	AudioCodec    string          `json:"audio_codec,omitempty"`
	Width         int             `json:"width,omitempty"`
	Height        int             `json:"height,omitempty"`
	FPS           float64         `json:"fps,omitempty"`
}

type CapabilityState string

const (
	CapabilityStateOK                CapabilityState = "ok"
	CapabilityStatePartial           CapabilityState = "partial"
	CapabilityStateFailed            CapabilityState = "failed"
	CapabilityStateInactiveEventFeed CapabilityState = "inactive_event_feed"
)

const (
	successRetryWindow = 30 * 24 * time.Hour
	partialRetryWindow = 6 * time.Hour
	failureRetryWindow = 24 * time.Hour
)

func (c Capability) Normalized() Capability {
	c.ServiceRef = normalize.ServiceRef(c.ServiceRef)
	c.Resolution = strings.TrimSpace(c.Resolution)
	c.Container = normalizeContainer(c.Container)
	c.VideoCodec = normalize.Token(c.VideoCodec)
	c.AudioCodec = normalize.Token(c.AudioCodec)
	c.Codec = normalize.Token(c.Codec)

	// Legacy rows only stored a single codec column. Treat it as video codec
	// when no richer truth is present yet.
	if c.VideoCodec == "" && c.AudioCodec == "" && c.Codec != "" {
		c.VideoCodec = c.Codec
	}
	if c.Codec == "" {
		switch {
		case c.VideoCodec != "":
			c.Codec = c.VideoCodec
		case c.AudioCodec != "":
			c.Codec = c.AudioCodec
		}
	}
	if c.Resolution == "" && c.Width > 0 && c.Height > 0 {
		c.Resolution = fmt.Sprintf("%dx%d", c.Width, c.Height)
	}

	state := c.State
	switch state {
	case CapabilityStateOK, CapabilityStatePartial, CapabilityStateFailed, CapabilityStateInactiveEventFeed:
	default:
		state = inferCapabilityState(c.Resolution, c.Codec)
	}
	c.State = state

	if c.LastAttempt.IsZero() && !c.LastScan.IsZero() {
		c.LastAttempt = c.LastScan
	}
	if c.LastSuccess.IsZero() && state != CapabilityStateFailed && state != CapabilityStateInactiveEventFeed && !c.LastScan.IsZero() {
		c.LastSuccess = c.LastScan
	}
	if c.LastScan.IsZero() && !c.LastSuccess.IsZero() {
		c.LastScan = c.LastSuccess
	}
	if c.NextRetryAt.IsZero() {
		anchor := c.LastAttempt
		if anchor.IsZero() {
			anchor = c.LastSuccess
		}
		if anchor.IsZero() {
			anchor = c.LastScan
		}
		if !anchor.IsZero() {
			c.NextRetryAt = anchor.Add(defaultRetryDelay(state))
		}
	}

	return c
}

func (c Capability) RetryDue(now time.Time) bool {
	normalized := c.Normalized()
	if normalized.NextRetryAt.IsZero() {
		return true
	}
	return !now.Before(normalized.NextRetryAt)
}

func (c Capability) Usable() bool {
	switch c.Normalized().State {
	case CapabilityStateOK, CapabilityStatePartial:
		return true
	default:
		return false
	}
}

func (c Capability) IsInactiveEventFeed() bool {
	return c.Normalized().State == CapabilityStateInactiveEventFeed
}

func (c Capability) HasMediaTruth() bool {
	normalized := c.Normalized()
	return normalized.Container != "" && normalized.VideoCodec != "" && normalized.AudioCodec != ""
}

func inferCapabilityState(resolution, codec string) CapabilityState {
	hasResolution := resolution != ""
	hasCodec := codec != ""
	switch {
	case hasResolution && hasCodec:
		return CapabilityStateOK
	case hasResolution || hasCodec:
		return CapabilityStatePartial
	default:
		return CapabilityStateFailed
	}
}

func normalizeContainer(in string) string {
	switch normalize.Token(in) {
	case "mpegts":
		return "ts"
	default:
		return normalize.Token(in)
	}
}

func defaultRetryDelay(state CapabilityState) time.Duration {
	switch state {
	case CapabilityStatePartial:
		return partialRetryWindow
	case CapabilityStateFailed:
		return failureRetryWindow
	case CapabilityStateInactiveEventFeed:
		return failureRetryWindow
	default:
		return successRetryWindow
	}
}

// CapabilityStore defines the interface for hardware metadata persistence.
type CapabilityStore interface {
	Update(cap Capability)
	Get(serviceRef string) (Capability, bool)
	Close() error
}

// NewStore creates a CapabilityStore based on the backend.
// Per ADR-021: Only sqlite backend is supported in production.
func NewStore(backend, storagePath string) (CapabilityStore, error) {
	if backend == "" {
		backend = "sqlite" // Default: SQLite is Single Durable Truth (ADR-020, ADR-021)
	}

	switch backend {
	case "sqlite":
		return NewSqliteStore(filepath.Join(storagePath, "capabilities.sqlite"))
	case "memory":
		return NewMemoryStore(), nil
	case "json": // ADR-021 removed
		// ADR-021: JSON file backend is DEPRECATED and removed.
		// See docs/ops/BACKUP_RESTORE.md for SQLite-only operations.
		return nil, fmt.Errorf("DEPRECATED: json backend removed (ADR-021). Only SQLite is supported in production")
	default:
		return nil, fmt.Errorf("unknown capability store backend: %s (supported: sqlite, memory)", backend)
	}
}

// MemoryStore implements an in-memory CapabilityStore (ephemeral).
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]Capability
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]Capability),
	}
}

func (s *MemoryStore) Update(cap Capability) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cap = cap.Normalized()
	s.data[cap.ServiceRef] = cap
}

func (s *MemoryStore) Get(serviceRef string) (Capability, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cap, ok := s.data[normalize.ServiceRef(serviceRef)]
	if !ok {
		return Capability{}, false
	}
	return cap.Normalized(), true
}

func (s *MemoryStore) Close() error {
	return nil
}
