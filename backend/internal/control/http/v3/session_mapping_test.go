// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestMapSessionState(t *testing.T) {
	cases := []struct {
		in   model.SessionState
		want SessionResponseState
	}{
		{model.SessionStarting, SessionResponseStateSTARTING},
		{model.SessionPriming, SessionResponseStatePRIMING},
		{model.SessionReady, SessionResponseStateREADY},
		{model.SessionDraining, SessionResponseStateDRAINING},
		{model.SessionStopping, SessionResponseStateSTOPPING},
		{model.SessionStopped, SessionResponseStateSTOPPED},
		{model.SessionFailed, SessionResponseStateFAILED},
		{model.SessionCancelled, SessionResponseStateCANCELLED},
		{model.SessionUnknown, SessionResponseStateIDLE},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, mapSessionState(tc.in))
	}
}

func TestMapSessionReason(t *testing.T) {
	cases := []struct {
		in   model.ReasonCode
		want SessionResponseReason
		ok   bool
	}{
		{model.RNone, SessionResponseReason(model.RNone), true},
		{model.RCancelled, SessionResponseReason(model.RCancelled), true},
		{model.RClientStop, SessionResponseReason(model.RClientStop), true},
		{model.RIdleTimeout, SessionResponseReason(model.RIdleTimeout), true},
		{model.RTuneTimeout, SessionResponseReason(model.RTuneTimeout), true},
	}

	for _, tc := range cases {
		got, ok := mapSessionReason(tc.in)
		require.Equal(t, tc.ok, ok)
		require.Equal(t, tc.want, got)
	}
}

func TestMapTerminalProblem(t *testing.T) {
	got := mapTerminalProblem(lifecycle.PublicOutcome{
		State:      model.SessionFailed,
		Reason:     model.RProcessEnded,
		DetailCode: model.DTranscodeStalled,
	})

	require.Equal(t, "error/transcode_stalled", got.problemType)
	require.Equal(t, "TRANSCODE_STALLED", got.code)
	require.Equal(t, "Transcode stalled - no progress detected", got.title)
	require.Equal(t, "The session failed because the transcode process stopped producing progress.", got.detail)
}
