package read

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/platform/paths"
)

// PlaylistFileReader allows mocking file reads for testing.
// Defaults to os.ReadFile.
var PlaylistFileReader = os.ReadFile

// StreamsQuery defines filtering parameters for streams.
type StreamsQuery struct {
	IncludeClientIP bool
}

// StreamSession is a control-layer representation of an active stream.
type StreamSession struct {
	ID          string
	ChannelName string
	ServiceRef  string
	ClientIP    string
	StartedAt   time.Time
	State       string // "active" (strict, non-terminal)
	Program     string
	Description string
	StartTime   int64
	EndTime     int64
}

// StateStore defines the read interface needed from the session store.
type StateStore interface {
	ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
}

// GetStreams returns a list of active streams.
// Contract:
// - Always returns []StreamSession (never nil).
// - Sorted by StartedAt DESC (nulls last), then ID ASC.
// - ChannelName resolved best-effort from playlist.
// - ClientIP included only if requested (Gate A).
func GetStreams(ctx context.Context, cfg config.AppConfig, snap config.Snapshot, store StateStore, q StreamsQuery) ([]StreamSession, error) {
	// 1. Fetch sessions from store (Source of Truth)
	records, err := store.ListSessions(ctx)
	if err != nil {
		return []StreamSession{}, err
	}

	// 2. Resolve Channel Names (Best Effort)
	nameMap := make(map[string]string)
	playlistName := snap.Runtime.PlaylistFilename
	path, err := paths.ValidatePlaylistPath(cfg.DataDir, playlistName)
	if err != nil {
		return []StreamSession{}, err
	}
	if data, err := PlaylistFileReader(path); err == nil {
		channels := m3u.Parse(string(data))
		for _, ch := range channels {
			// Note A: Centralized ServiceRef extraction to prevent drift
			ref := ExtractServiceRef(ch.URL, ch.TvgID)

			displayName := ch.Name
			if displayName == "" {
				displayName = ch.TvgID
			}
			if ref != "" {
				nameMap[ref] = displayName
			}
		}
	}

	// 3. Filter and Map
	var sessions []StreamSession
	for _, r := range records {
		// Filter terminal sessions
		if r.State.IsTerminal() {
			continue
		}

		// Map State: Domain → Contract
		// Use the deterministic truth engine (PR-P3-2)
		lifecycleState := model.DeriveLifecycleState(r, time.Now())

		// Canonicalize: running states → "active", non-running → filter, unknown → fail
		contractState, err := canonicalRunningState(r.SessionID, lifecycleState)
		if err != nil {
			// Fail-closed: unknown state leaked into provider
			return []StreamSession{}, fmt.Errorf("state canonicalization failed: %w", err)
		}
		if contractState == "" {
			// Non-running state (stalled/ending/idle/error) → filter out
			continue
		}

		// Resolve Name
		name := nameMap[r.ServiceRef]
		if name == "" {
			name = r.ServiceRef // Fallback
		}

		// Resolve IP (Gated)
		ip := ""
		if q.IncludeClientIP {
			if val, ok := r.ContextData["client_ip"]; ok {
				ip = val
			}
		}

		// StartedAt
		var startedAt time.Time
		if r.CreatedAtUnix > 0 {
			startedAt = time.Unix(r.CreatedAtUnix, 0)
		}

		sessions = append(sessions, StreamSession{
			ID:          r.SessionID,
			ChannelName: name,
			ServiceRef:  r.ServiceRef,
			ClientIP:    ip,
			StartedAt:   startedAt,
			State:       contractState,
		})
	}

	// 4. Deterministic Sort
	// Primary: StartedAt DESC (Zero last)
	// Secondary: ID ASC
	sort.Slice(sessions, func(i, j int) bool {
		t1 := sessions[i].StartedAt
		t2 := sessions[j].StartedAt

		// Compare times
		if !t1.Equal(t2) {
			// Zero handling explicitly if needed, but unix 0 is just "old".
			// User said "startedAt nil/0 -> ganz nach hinten" (very end).
			// If we sort DESC (Latest first), then 0 (1970) is naturally last.
			// Unless we have future dates? Unlikely.
			// So t1 > t2 ensures t1 comes first.
			return t1.After(t2)
		}

		// Tie-breaker: ID ASC
		// If times equal (or both zero), sort by ID
		return sessions[i].ID < sessions[j].ID
	})

	// 5. Empty Shape (Always [])
	if sessions == nil {
		return []StreamSession{}, nil
	}

	return sessions, nil
}

// Local helper duplicated from services.go to capture service ref logic if needed.
// Only used if we want precise URL based mapping.
// For now, we rely on TvgID map above.
