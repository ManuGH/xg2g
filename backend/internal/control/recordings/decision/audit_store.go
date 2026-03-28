package decision

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type MemoryAuditStore struct {
	mu      sync.Mutex
	current map[memoryAuditKey]memoryAuditCurrent
	history map[memoryAuditKey][]Event
}

type memoryAuditKey struct {
	ServiceRef      string
	SubjectKind     string
	Origin          string
	ClientFamily    string
	RequestedIntent string
}

type memoryAuditCurrent struct {
	Event      Event
	ChangedAt  time.Time
	LastSeenAt time.Time
}

func NewAuditStore(backend, storagePath string) (EventSink, error) {
	switch backend {
	case "", "sqlite":
		return NewSqliteAuditStore(filepath.Join(storagePath, "decision_audit.sqlite"))
	case "memory":
		return NewMemoryAuditStore(), nil
	default:
		return nil, fmt.Errorf("unknown decision audit store backend: %s (supported: sqlite, memory)", backend)
	}
}

func NewMemoryAuditStore() *MemoryAuditStore {
	return &MemoryAuditStore{
		current: make(map[memoryAuditKey]memoryAuditCurrent),
		history: make(map[memoryAuditKey][]Event),
	}
}

func (s *MemoryAuditStore) Record(_ context.Context, event Event) error {
	event = event.Normalized()
	if err := event.Valid(); err != nil {
		return err
	}

	key := memoryAuditKey{
		ServiceRef:      event.ServiceRef,
		SubjectKind:     normalizeSubjectKind(event.SubjectKind),
		Origin:          normalizeEventOrigin(event.Origin),
		ClientFamily:    normalizeClientFamily(event.ClientFamily),
		RequestedIntent: normalizeRequestedIntent(event.RequestedIntent),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, found := s.current[key]
	outputChanged := !found || current.Event.OutputHash != event.OutputHash
	if !found || !event.DecidedAt.Before(current.LastSeenAt) {
		changedAt := event.DecidedAt
		if found && !outputChanged {
			changedAt = current.ChangedAt
		}
		s.current[key] = memoryAuditCurrent{
			Event:      event,
			ChangedAt:  changedAt,
			LastSeenAt: event.DecidedAt,
		}
	}

	if outputChanged {
		s.history[key] = append(s.history[key], event)
	}
	s.pruneLocked(time.Now().UTC().Add(-historyRetention))
	return nil
}

func (s *MemoryAuditStore) pruneLocked(cutoff time.Time) {
	for key, events := range s.history {
		filtered := events[:0]
		for _, event := range events {
			if !event.DecidedAt.Before(cutoff) {
				filtered = append(filtered, event)
			}
		}
		if len(filtered) == 0 {
			delete(s.history, key)
			continue
		}
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].DecidedAt.After(filtered[j].DecidedAt)
		})
		if len(filtered) > historyEntriesPerKey {
			filtered = filtered[:historyEntriesPerKey]
		}
		s.history[key] = filtered
	}
}
