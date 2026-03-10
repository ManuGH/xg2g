// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type failingUpdateStateStore struct {
	store.StateStore
	err error
}

func (s failingUpdateStateStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	return nil, s.err
}

func TestRunExecutionLoop_StopsPipelineWhenPrimingTransitionFails(t *testing.T) {
	ctx := context.Background()
	primingErr := errors.New("priming update failed")
	pipe := testkit.NewStepperPipeline()
	orch := &Orchestrator{
		Store:               failingUpdateStateStore{StateStore: store.NewMemoryStore(), err: primingErr},
		Pipeline:            pipe,
		PipelineStopTimeout: time.Second,
	}

	session := &model.SessionRecord{}
	sessionCtx := &sessionContext{
		Mode:       model.ModeLive,
		ServiceRef: "ref:cleanup",
	}
	event := model.StartSessionEvent{
		SessionID:  "session-cleanup",
		ServiceRef: "ref:cleanup",
		ProfileID:  "p1",
	}

	errCh := make(chan error, 1)
	go func() {
		_, _, err := orch.runExecutionLoop(
			ctx,
			ctx,
			event,
			sessionCtx,
			session,
			time.Now(),
			zerolog.Nop(),
			func(string, model.ReasonCode) {},
			0,
		)
		errCh <- err
	}()

	<-pipe.StartCalled()
	pipe.AllowStart()

	require.NoError(t, <-pipe.StartReturned())
	require.ErrorIs(t, <-errCh, primingErr)

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case <-pipe.StopCalled():
	case <-waitCtx.Done():
		t.Fatalf("timed out waiting for pipeline stop after priming failure: %v", waitCtx.Err())
	}
}
