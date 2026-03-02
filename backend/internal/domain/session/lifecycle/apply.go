// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// ApplyTransition mutates the session record according to the transition.
func ApplyTransition(rec *model.SessionRecord, tr Transition, now time.Time) {
	rec.State = tr.To
	if tr.Reason != "" {
		rec.Reason = tr.Reason
	}
	if tr.Reason != "" {
		rec.ReasonDetailCode = tr.DetailCode
		rec.ReasonDetailDebug = tr.DetailDebug
	}
	if tr.To.IsTerminal() {
		if tr.To == model.SessionFailed {
			rec.PipelineState = model.PipeFail
		} else {
			rec.PipelineState = model.PipeStopped
		}
	}
	rec.UpdatedAtUnix = now.Unix()
}
