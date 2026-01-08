package read

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

// TimerState is intentionally stringy to match API enums cleanly.
type TimerState string

const (
	TimerStateScheduled TimerState = "scheduled"
	TimerStateRecording TimerState = "recording"
	TimerStateDisabled  TimerState = "disabled"
	TimerStateUnknown   TimerState = "unknown"

	// Optional (only if your OpenAPI supports them; otherwise keep internal only)
	TimerStateCompleted TimerState = "completed"
	TimerStateFailed    TimerState = "failed" // Receiver indicates failure (if supported)
	TimerStateCanceled  TimerState = "canceled"
)

// RawStateMeaning documents any known OpenWebIF state codes.
// IMPORTANT: These codes vary across images/skins. Keep conservative.
// If you do not have authoritative mapping yet, leave this empty and rely on time windows.
type RawStateMeaning string

const (
	rawCompleted RawStateMeaning = "completed"
	rawFailed    RawStateMeaning = "failed"
	rawCanceled  RawStateMeaning = "canceled"
)

// KnownTimerStates is a small, conservative map you can extend once verified.
// Start with empty or minimal. Only add once you confirm via receiver logs/UI.
var KnownTimerStates = map[int]RawStateMeaning{
	// Example placeholders (DO NOT trust unless verified on your image):
	// 0: rawCompleted,
	// 1: rawCanceled,
	// 2: rawFailed,
}

// MapOpenWebIFTimerState implements the “Timer Truth Table”.
func MapOpenWebIFTimerState(t openwebif.Timer, now time.Time) TimerState {
	// 1) Disabled is authoritative.
	if t.Disabled != 0 {
		return TimerStateDisabled
	}

	nowUN := now.Unix()

	// 2) In-progress window: best-effort recording.
	// Note: This assumes [Begin, End) is the intended record window.
	// If receiver shifts times, your read-back verification already tolerates +/- 5s.
	if t.Begin > 0 && t.End > 0 && nowUN >= t.Begin && nowUN < t.End {
		return TimerStateRecording
	}

	// 3) Future timers are scheduled.
	if t.Begin > 0 && nowUN < t.Begin {
		return TimerStateScheduled
	}

	// 4) Past timers: only claim specific states if we have verified raw mapping.
	if meaning, ok := KnownTimerStates[t.State]; ok {
		switch meaning {
		case rawCompleted:
			return TimerStateCompleted
		case rawFailed:
			return TimerStateFailed
		case rawCanceled:
			return TimerStateCanceled
		}
	}

	// 5) Otherwise unknown (fail-closed).
	return TimerStateUnknown
}

// Timer is a control-layer representation of a DVR timer.
type Timer struct {
	TimerID     string
	ServiceRef  string
	ServiceName string
	Name        string
	Description string
	Begin       int64
	End         int64
	State       TimerState
	DisabledRaw int
	RawState    int
	Filename    string
}

// MapOpenWebIFTimerToDTO maps an OpenWebIF timer to your control-layer timer model.
// This avoids system.go doing ad-hoc mapping.
func MapOpenWebIFTimerToDTO(t openwebif.Timer, now time.Time) Timer {
	return Timer{
		TimerID:     MakeTimerID(t.ServiceRef, t.Begin, t.End),
		ServiceRef:  t.ServiceRef,
		ServiceName: t.ServiceName,
		Name:        t.Name,
		Description: t.Description,
		Begin:       t.Begin,
		End:         t.End,
		State:       MapOpenWebIFTimerState(t, now),
		DisabledRaw: t.Disabled,
		RawState:    t.State,
		Filename:    t.Filename,
	}
}

// TimersSource defines the interface needed to fetch timer data.
type TimersSource interface {
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
}

// TimersQuery defines filtering parameters for timers.
type TimersQuery struct {
	State TimerState
	From  int64
}

// GetTimers returns a list of timers filtered by state.
func GetTimers(ctx context.Context, src TimersSource, q TimersQuery, clock Clock) ([]Timer, error) {
	timers, err := src.GetTimers(ctx)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	mapped := make([]Timer, 0, len(timers))
	for _, t := range timers {
		dto := MapOpenWebIFTimerToDTO(t, now)

		if q.State != "" {
			if dto.State != q.State {
				continue
			}
		}

		// Filtering by 'from' timestamp (if provided)
		if q.From > 0 && dto.End < q.From {
			continue
		}

		mapped = append(mapped, dto)
	}

	return mapped, nil
}
