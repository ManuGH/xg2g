// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package read

import (
	"fmt"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// canonicalRunningState maps internal lifecycle states to contract states.
// Contract: GetStreams returns only "running" sessions with state "active".
//
// Returns:
//   - "active": for running states (starting, buffering, active)
//   - "": for non-running states (should be filtered from list)
//   - error: for unknown states (fail-closed)
func canonicalRunningState(sessionID string, state model.LifecycleState) (string, error) {
	switch state {
	case model.LifecycleStarting, model.LifecycleBuffering, model.LifecycleActive:
		return "active", nil
	case model.LifecycleStalled, model.LifecycleEnding, model.LifecycleIdle, model.LifecycleError:
		// Non-running states: must not appear in GetStreams list
		return "", nil
	default:
		return "", fmt.Errorf("unknown lifecycle state %q for session %s", state, sessionID)
	}
}
