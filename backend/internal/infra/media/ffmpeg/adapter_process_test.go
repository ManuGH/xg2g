// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// A normal user stop or watchdog termination kills ffmpeg (procErr != nil) and
// may carry a latched transient vaapi/nvenc warning line, but that is NOT an
// encoder failure and must not feed the sticky GPU->CPU demotion counter.
func TestShouldRecordHWRuntimeFailure(t *testing.T) {
	exitErr := errors.New("exit status 1")
	const failLine = "vaapi: encode failed"

	cases := []struct {
		name        string
		naturalExit bool
		procErr     error
		failureLine string
		want        bool
	}{
		{"natural non-zero exit with failure line records", true, exitErr, failLine, true},
		{"natural clean exit (code 0) does not record", true, nil, failLine, false},
		{"no failure line never records", true, exitErr, "", false},
		{"user-initiated stop does not record", false, exitErr, failLine, false},
		{"watchdog stall termination does not record", false, exitErr, failLine, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, shouldRecordHWRuntimeFailure(tc.naturalExit, tc.procErr, tc.failureLine))
		})
	}
}

// awaitProcessExit classifies how ffmpeg ended. These tests drive each select
// branch deterministically (only the channel for the target case is primed) and
// assert naturalExit is set ONLY for a true self-exit.

func TestAwaitProcessExit_NaturalSelfExit(t *testing.T) {
	procErrCh := make(chan error, 1)
	wdErrCh := make(chan error, 1)
	procErrCh <- errors.New("exit status 1")

	out := awaitProcessExit(context.Background(), procErrCh, wdErrCh,
		func(error) { t.Fatal("stall callback must not fire") },
		func() { t.Fatal("cancel callback must not fire") },
	)

	require.True(t, out.naturalExit, "a self-terminated process is a natural exit")
	require.Error(t, out.procErr)
	require.False(t, out.watchdogConsumed)
}

func TestAwaitProcessExit_WatchdogStallIsNotNatural(t *testing.T) {
	procErrCh := make(chan error, 1)
	wdErrCh := make(chan error, 1)
	wdErrCh <- errors.New("stall: no progress")
	go func() { procErrCh <- errors.New("signal: killed") }()

	stalled := false
	out := awaitProcessExit(context.Background(), procErrCh, wdErrCh,
		func(error) { stalled = true },
		func() { t.Fatal("cancel callback must not fire") },
	)

	require.False(t, out.naturalExit, "watchdog stall is a deliberate termination")
	require.True(t, stalled, "stall callback must fire to terminate the process")
	require.True(t, out.watchdogConsumed)
}

func TestAwaitProcessExit_ParentCtxCancelIsNotNatural(t *testing.T) {
	procErrCh := make(chan error, 1)
	wdErrCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	go func() { procErrCh <- errors.New("signal: killed") }()

	canceled := false
	out := awaitProcessExit(ctx, procErrCh, wdErrCh,
		func(error) { t.Fatal("stall callback must not fire") },
		func() { canceled = true },
	)

	require.False(t, out.naturalExit, "a user stop via parentCtx is not a natural exit")
	require.True(t, canceled, "cancel callback must fire to terminate the process")
}

// Regression guard for the bug this refactor fixes: a user stop cancels
// parentCtx, and because the watchdog context is derived from parentCtx the
// watchdog returns nil -> wdErrCh receives nil. That branch is a context
// cancellation, NOT a natural exit, and previously set naturalExit=true (so
// ~half of user stops, by the random select, were misclassified and could feed
// the GPU demotion counter).
func TestAwaitProcessExit_WatchdogNilFromCtxCancelIsNotNatural(t *testing.T) {
	procErrCh := make(chan error, 1)
	wdErrCh := make(chan error, 1)
	wdErrCh <- nil
	go func() { procErrCh <- errors.New("signal: killed") }()

	out := awaitProcessExit(context.Background(), procErrCh, wdErrCh,
		func(error) { t.Fatal("stall callback must not fire (wdErr was nil)") },
		func() { t.Fatal("cancel callback must not fire (handled via wdErrCh)") },
	)

	require.False(t, out.naturalExit, "wdErr==nil is a context cancel, not a natural exit")
	require.True(t, out.watchdogConsumed)
}
