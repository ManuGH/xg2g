package read

import (
	"context"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

// TimersSource defines the interface needed to fetch timer data.
type TimersSource interface {
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
}

// TimersQuery defines filtering parameters for timers.
type TimersQuery struct {
	State string
	From  int64
}

// TimerStates
const (
	TimerStateScheduled = "scheduled"
	TimerStateRecording = "recording"
	TimerStateCompleted = "completed"
	TimerStateDisabled  = "disabled"
	TimerStateUnknown   = "unknown"
)

// Timer is a control-layer representation of a DVR timer.
type Timer struct {
	TimerID     string
	ServiceRef  string
	ServiceName string
	Name        string
	Description string
	Begin       int64
	End         int64
	State       string
}

// GetTimers returns a list of timers filtered by state.
func GetTimers(ctx context.Context, src TimersSource, q TimersQuery, clock Clock) ([]Timer, error) {
	timers, err := src.GetTimers(ctx)
	if err != nil {
		return nil, err
	}

	mapped := make([]Timer, 0, len(timers))
	for _, t := range timers {
		stateStr := TimerStateUnknown
		switch t.State {
		case 0:
			stateStr = TimerStateScheduled
			if t.Disabled != 0 {
				stateStr = TimerStateDisabled
			}
		case 2:
			stateStr = TimerStateRecording
		case 3:
			stateStr = TimerStateCompleted
		default:
			if t.Disabled != 0 {
				stateStr = TimerStateDisabled
			}
		}

		if q.State != "" && q.State != "all" {
			if stateStr != q.State {
				continue
			}
		}

		// Filtering by 'from' timestamp (if provided)
		if q.From > 0 && t.End < q.From {
			continue
		}

		timerID := MakeTimerID(t.ServiceRef, t.Begin, t.End)
		mapped = append(mapped, Timer{
			TimerID:     timerID,
			ServiceRef:  t.ServiceRef,
			ServiceName: t.ServiceName,
			Name:        t.Name,
			Description: t.Description,
			Begin:       t.Begin,
			End:         t.End,
			State:       stateStr,
		})
	}

	return mapped, nil
}
