// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import "github.com/ManuGH/xg2g/internal/domain/session/model"

// ForbiddenTransitionReason documents why a transition is disallowed.
func ForbiddenTransitionReason(from model.SessionState, ev EventKind) string {
	decision, ok := DecisionFor(from, ev)
	if !ok {
		return ""
	}
	if decision.Allowed {
		return ""
	}
	return decision.Reason
}
