// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// Dispatch resolves the next transition (and applies it) from the SSOT rules.
// It is the only entry point that interprets cause/stopIntent/phase.
func Dispatch(rec *model.SessionRecord, phase Phase, ev Event, cause error, stopIntent bool, now time.Time) (Transition, error) {
	if rec.State.IsTerminal() {
		return illegalTransition(rec, rec.State, ev.Kind, now)
	}
	if ev.Kind == EvTerminalize {
		out := TerminalOutcome(stopIntent, phase, cause)
		tr := Transition{
			From:   rec.State,
			To:     out.State,
			Event:  EvTerminalize,
			Reason: out.Reason,
			DetailCode: out.DetailCode,
			DetailDebug: out.DetailDebug,
		}
		ApplyTransition(rec, tr, now)
		return tr, nil
	}

	decision, ok := DecisionFor(rec.State, ev.Kind)
	if !ok || !decision.Allowed {
		return illegalTransition(rec, rec.State, ev.Kind, now)
	}
	tr, ok := TransitionFor(rec.State, ev.Kind)
	if !ok {
		return illegalTransition(rec, rec.State, ev.Kind, now)
	}

	if ev.Reason != "" {
		tr.Reason = ev.Reason
	}
	if ev.DetailCode != "" {
		tr.DetailCode = ev.DetailCode
	}

	ApplyTransition(rec, tr, now)
	return tr, nil
}

// EventFromCause derives a terminalization event from cause/phase/stopIntent.
func EventFromCause(phase Phase, cause error, stopIntent bool) Event {
	if stopIntent {
		return Event{Kind: EvTerminalize, Reason: model.RClientStop}
	}
	if errors.Is(cause, context.Canceled) {
		return Event{Kind: EvTerminalize, Reason: model.RCancelled}
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		if phase == PhaseStart {
			return Event{Kind: EvTerminalize, Reason: model.RTuneTimeout}
		}
		return Event{Kind: EvTerminalize, Reason: model.RDeadlineExceeded}
	}
	return Event{Kind: EvTerminalize}
}
