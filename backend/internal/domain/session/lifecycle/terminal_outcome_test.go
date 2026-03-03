// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
)

func TestTerminalOutcome_TruthTable(t *testing.T) {
	t.Run("stop intent wins over cancel", func(t *testing.T) {
		out := TerminalOutcome(true, PhaseStart, context.Canceled)
		assert.Equal(t, model.SessionStopped, out.State)
		assert.Equal(t, model.RClientStop, out.Reason)
		assert.Equal(t, model.DNone, out.DetailCode)
	})

	t.Run("stop intent wins over deadline exceeded", func(t *testing.T) {
		out := TerminalOutcome(true, PhaseStart, context.DeadlineExceeded)
		assert.Equal(t, model.SessionStopped, out.State)
		assert.Equal(t, model.RClientStop, out.Reason)
		assert.Equal(t, model.DNone, out.DetailCode)
	})

	t.Run("plain cancel maps to cancelled", func(t *testing.T) {
		out := TerminalOutcome(false, PhaseStart, context.Canceled)
		assert.Equal(t, model.SessionCancelled, out.State)
		assert.Equal(t, model.RCancelled, out.Reason)
		assert.Equal(t, model.DContextCanceled, out.DetailCode)
	})

	t.Run("deadline exceeded during start maps to tune timeout", func(t *testing.T) {
		out := TerminalOutcome(false, PhaseStart, context.DeadlineExceeded)
		assert.Equal(t, model.SessionFailed, out.State)
		assert.Equal(t, model.RTuneTimeout, out.Reason)
		assert.Equal(t, model.DDeadlineExceeded, out.DetailCode)
	})

	t.Run("deadline exceeded outside start is distinct", func(t *testing.T) {
		out := TerminalOutcome(false, PhaseRunning, context.DeadlineExceeded)
		assert.Equal(t, model.SessionFailed, out.State)
		assert.Equal(t, model.RDeadlineExceeded, out.Reason)
		assert.Equal(t, model.DDeadlineExceeded, out.DetailCode)
	})

	t.Run("vod completion returns draining", func(t *testing.T) {
		out := TerminalOutcome(false, PhaseVODComplete, nil)
		assert.Equal(t, model.SessionDraining, out.State)
		assert.Equal(t, model.RNone, out.Reason)
		assert.Equal(t, model.DRecordingComplete, out.DetailCode)
	})
}

func TestTerminalOutcome_StopWinsRace(t *testing.T) {
	var stopIntent atomic.Bool
	type errHolder struct{ err error }
	errVal := atomic.Value{}
	errVal.Store(errHolder{err: nil})

	start := make(chan struct{})
	done := make(chan struct{}, 2)

	go func() {
		<-start
		stopIntent.Store(true)
		done <- struct{}{}
	}()
	go func() {
		<-start
		errVal.Store(errHolder{err: context.Canceled})
		done <- struct{}{}
	}()

	close(start)
	<-done
	<-done

	holder := errVal.Load().(errHolder)
	out := TerminalOutcome(stopIntent.Load(), PhaseStart, holder.err)
	assert.Equal(t, model.SessionStopped, out.State)
	assert.Equal(t, model.RClientStop, out.Reason)
	assert.Equal(t, model.DNone, out.DetailCode)
}
