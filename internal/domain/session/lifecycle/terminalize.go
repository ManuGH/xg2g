// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"context"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// Outcome is the canonical terminal mapping for a session.
type Outcome struct {
	State  model.SessionState
	Reason model.ReasonCode
	DetailCode  model.ReasonDetailCode
	DetailDebug string
}

// TerminalOutcome is the single source of truth for terminal session outcomes.
func TerminalOutcome(stopIntent bool, phase Phase, err error) Outcome {
	if stopIntent {
		return Outcome{
			State:        model.SessionStopped,
			Reason:       model.RClientStop,
			DetailCode:   model.DNone,
			DetailDebug:  "",
		}
	}

	if err == nil {
		if phase == PhaseVODComplete {
			return Outcome{
				State:       model.SessionDraining,
				Reason:      model.RNone,
				DetailCode:  model.DRecordingComplete,
				DetailDebug: "",
			}
		}
		return Outcome{
			State:       model.SessionFailed,
			Reason:      model.RProcessEnded,
			DetailCode:  model.DNone,
			DetailDebug: "",
		}
	}

	if errors.Is(err, context.Canceled) {
		return Outcome{
			State:       model.SessionCancelled,
			Reason:      model.RCancelled,
			DetailCode:  model.DContextCanceled,
			DetailDebug: "",
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		if phase == PhaseStart {
			return Outcome{
				State:       model.SessionFailed,
				Reason:      model.RTuneTimeout,
				DetailCode:  model.DDeadlineExceeded,
				DetailDebug: "",
			}
		}
		return Outcome{
			State:       model.SessionFailed,
			Reason:      model.RDeadlineExceeded,
			DetailCode:  model.DDeadlineExceeded,
			DetailDebug: "",
		}
	}

	reason, detailCode, detailDebug := ClassifyReason(err)
	state := model.SessionFailed
	if reason == model.RClientStop {
		state = model.SessionStopped
		detailCode = model.DNone
		detailDebug = ""
	} else if reason == model.RCancelled {
		state = model.SessionCancelled
		detailCode = model.DContextCanceled
		detailDebug = ""
	}

	return Outcome{
		State:       state,
		Reason:      reason,
		DetailCode:  detailCode,
		DetailDebug: detailDebug,
	}
}

// ApplyOutcome mutates the session record with the canonical outcome.
func ApplyOutcome(r *model.SessionRecord, out Outcome) {
	r.State = out.State
	if out.State == model.SessionFailed {
		r.PipelineState = model.PipeFail
	} else {
		r.PipelineState = model.PipeStopped
	}
	r.Reason = out.Reason
	r.ReasonDetailCode = out.DetailCode
	r.ReasonDetailDebug = out.DetailDebug
}
