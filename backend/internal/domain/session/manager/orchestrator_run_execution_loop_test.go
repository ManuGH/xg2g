// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
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

type startupRetryPipeline struct {
	hlsRoot              string
	secondStartCalled    chan struct{}
	allowSecondArtifacts chan struct{}
	startCount           atomic.Int32
	stopCount            atomic.Int32
}

func newStartupRetryPipeline(hlsRoot string) *startupRetryPipeline {
	return &startupRetryPipeline{
		hlsRoot:              hlsRoot,
		secondStartCalled:    make(chan struct{}),
		allowSecondArtifacts: make(chan struct{}),
	}
}

func (p *startupRetryPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	attempt := int(p.startCount.Add(1))
	handle := ports.RunHandle(fmt.Sprintf("%s-attempt-%d", spec.SessionID, attempt))

	switch attempt {
	case 1:
		if err := p.writeReadyArtifacts(spec.SessionID, "stale-first-attempt"); err != nil {
			return "", err
		}
	case 2:
		close(p.secondStartCalled)
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-p.allowSecondArtifacts:
				_ = p.writeReadyArtifacts(spec.SessionID, "fresh-second-attempt")
			}
		}()
	}

	return handle, nil
}

func (p *startupRetryPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		p.stopCount.Add(1)
		return nil
	}
}

func (p *startupRetryPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	select {
	case <-ctx.Done():
		return ports.HealthStatus{Healthy: false, Message: ctx.Err().Error()}
	default:
	}

	if strings.HasSuffix(string(handle), "-attempt-1") {
		return ports.HealthStatus{Healthy: false, Message: "upstream stream ended prematurely"}
	}
	return ports.HealthStatus{Healthy: true}
}

func (p *startupRetryPipeline) writeReadyArtifacts(sessionID, payload string) error {
	sessionDir := filepath.Join(p.hlsRoot, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}

	playlist := `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:6.000000,
seg_000000.ts
#EXTINF:6.000000,
seg_000001.ts
#EXTINF:6.000000,
seg_000002.ts
`
	if err := os.WriteFile(filepath.Join(sessionDir, "index.m3u8"), []byte(playlist), 0o600); err != nil {
		return err
	}
	for _, name := range []string{"seg_000000.ts", "seg_000001.ts", "seg_000002.ts"} {
		if err := os.WriteFile(filepath.Join(sessionDir, name), []byte(payload), 0o600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(sessionDir, model.SessionFirstFrameMarkerFilename), []byte("ready"), 0o600); err != nil {
		return err
	}
	return nil
}

func TestRunExecutionLoop_RetriesEarlyUpstreamProcessExitOnceAndCleansStaleArtifacts(t *testing.T) {
	ctx := context.Background()
	hlsRoot := t.TempDir()
	st := store.NewMemoryStore()
	pipe := newStartupRetryPipeline(hlsRoot)
	orch := &Orchestrator{
		Store:               st,
		Pipeline:            pipe,
		HLSRoot:             hlsRoot,
		LiveReadySegments:   3,
		Platform:            NewTestPlatform(hlsRoot),
		PipelineStopTimeout: time.Second,
	}

	const sid = "session-retry"
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sid,
		ServiceRef: "ref:retry",
		State:      model.SessionStarting,
		Profile:    model.ProfileSpec{Name: "high"},
	}))

	session, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, session)

	errCh := make(chan error, 1)
	handleCh := make(chan ports.RunHandle, 1)
	go func() {
		handle, _, err := orch.runExecutionLoop(
			ctx,
			ctx,
			model.StartSessionEvent{
				SessionID:  sid,
				ServiceRef: "ref:retry",
				ProfileID:  "high",
			},
			&sessionContext{
				Mode:       model.ModeLive,
				ServiceRef: "ref:retry",
			},
			session,
			time.Now(),
			zerolog.Nop(),
			func(string, model.ReasonCode) {},
			0,
		)
		if err == nil {
			handleCh <- handle
		}
		errCh <- err
	}()

	select {
	case <-pipe.secondStartCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second startup attempt")
	}

	select {
	case err := <-errCh:
		t.Fatalf("runExecutionLoop returned before second attempt wrote fresh artifacts: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	close(pipe.allowSecondArtifacts)

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for successful retry completion")
	}

	select {
	case handle := <-handleCh:
		require.Equal(t, ports.RunHandle("session-retry-attempt-2"), handle)
	default:
		t.Fatal("expected successful second-attempt handle")
	}

	require.Equal(t, int32(2), pipe.startCount.Load())
	require.Equal(t, int32(1), pipe.stopCount.Load())
}
