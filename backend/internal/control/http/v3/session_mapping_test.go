// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestMapSessionState(t *testing.T) {
	cases := []struct {
		in   model.SessionState
		want SessionResponseState
	}{
		{model.SessionStarting, STARTING},
		{model.SessionPriming, PRIMING},
		{model.SessionReady, READY},
		{model.SessionDraining, DRAINING},
		{model.SessionStopping, STOPPING},
		{model.SessionStopped, STOPPED},
		{model.SessionFailed, FAILED},
		{model.SessionCancelled, CANCELLED},
		{model.SessionUnknown, IDLE},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, mapSessionState(tc.in))
	}
}

func TestMapSessionReason(t *testing.T) {
	cases := []struct {
		in    model.ReasonCode
		want  SessionResponseReason
		ok    bool
	}{
		{model.RNone, SessionResponseReason(model.RNone), true},
		{model.RCancelled, SessionResponseReason(model.RCancelled), true},
		{model.RClientStop, SessionResponseReason(model.RClientStop), true},
		{model.RTuneTimeout, SessionResponseReason(model.RTuneTimeout), true},
	}

	for _, tc := range cases {
		got, ok := mapSessionReason(tc.in)
		require.Equal(t, tc.ok, ok)
		require.Equal(t, tc.want, got)
	}
}
