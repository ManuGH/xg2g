// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
)

// PublicOutcome is the canonical, public-safe outcome derived from a session record.
// HTTP must serialize this 1:1 without interpretation.
type PublicOutcome struct {
	State      model.SessionState
	Reason     model.ReasonCode
	DetailCode model.ReasonDetailCode
}

// PublicOutcomeFromRecord derives a canonical outcome for external surfaces.
func PublicOutcomeFromRecord(r *model.SessionRecord) PublicOutcome {
	code := r.ReasonDetailCode
	if code == "" {
		code = model.DNone
		if r.ReasonDetailDebug != "" {
			log.L().Warn().Str("sessionId", r.SessionID).Msg("missing reason_detail_code; using D_NONE")
		}
	}

	// STOPPED must never expose cancel-style details.
	if r.State == model.SessionStopped && (code == model.DContextCanceled || code == model.DDeadlineExceeded) {
		log.L().Warn().Str("sessionId", r.SessionID).Msg("corrected stopped detail_code")
		code = model.DNone
	}

	return PublicOutcome{
		State:      r.State,
		Reason:     r.Reason,
		DetailCode: code,
	}
}
