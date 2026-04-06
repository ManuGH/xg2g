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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
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

	require.ErrorIs(t, <-errCh, primingErr)

	select {
	case <-pipe.StartCalled():
		t.Fatal("pipeline start should not be attempted when priming transition fails")
	default:
	}

	select {
	case <-pipe.StopCalled():
		t.Fatal("pipeline stop should not be attempted when pipeline start never happened")
	default:
	}
}

type startupRetryPipeline struct {
	hlsRoot              string
	secondStartCalled    chan struct{}
	allowSecondStart     chan struct{}
	allowSecondArtifacts chan struct{}
	secondArtifactsReady chan struct{}
	startCount           atomic.Int32
	stopCount            atomic.Int32
}

func newStartupRetryPipeline(hlsRoot string) *startupRetryPipeline {
	return &startupRetryPipeline{
		hlsRoot:              hlsRoot,
		secondStartCalled:    make(chan struct{}),
		allowSecondStart:     make(chan struct{}),
		allowSecondArtifacts: make(chan struct{}),
		secondArtifactsReady: make(chan struct{}),
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
		<-p.allowSecondStart
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-p.allowSecondArtifacts:
				_ = p.writeReadyArtifacts(spec.SessionID, "fresh-second-attempt")
				close(p.secondArtifactsReady)
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
	}

	sessionDir := filepath.Join(hlsRoot, "sessions", sid)
	for _, name := range []string{
		"index.m3u8",
		"seg_000000.ts",
		"seg_000001.ts",
		"seg_000002.ts",
		model.SessionFirstFrameMarkerFilename,
	} {
		_, statErr := os.Stat(filepath.Join(sessionDir, name))
		require.ErrorIs(t, statErr, os.ErrNotExist, "stale startup artifact %s should be cleaned before retry", name)
	}

	select {
	case err := <-errCh:
		t.Fatalf("runExecutionLoop returned before second attempt was released: %v", err)
	default:
	}

	close(pipe.allowSecondStart)

	close(pipe.allowSecondArtifacts)
	<-pipe.secondArtifactsReady

	select {
	case err := <-errCh:
		require.NoError(t, err)
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

type startupProfileFallbackPipeline struct {
	hlsRoot    string
	startCount atomic.Int32
	stopCount  atomic.Int32
	mu         sync.Mutex
	profiles   []string
}

func (p *startupProfileFallbackPipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	attempt := int(p.startCount.Add(1))

	p.mu.Lock()
	p.profiles = append(p.profiles, spec.Profile.Name)
	p.mu.Unlock()

	if attempt == 2 {
		if err := p.writeReadyArtifacts(spec.SessionID); err != nil {
			return "", err
		}
	}

	return ports.RunHandle(fmt.Sprintf("%s-attempt-%d", spec.SessionID, attempt)), nil
}

func (p *startupProfileFallbackPipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		p.stopCount.Add(1)
		return nil
	}
}

func (p *startupProfileFallbackPipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	select {
	case <-ctx.Done():
		return ports.HealthStatus{Healthy: false, Message: ctx.Err().Error()}
	default:
	}

	if strings.HasSuffix(string(handle), "-attempt-1") {
		return ports.HealthStatus{Healthy: false, Message: "copy output missing codec parameters"}
	}
	return ports.HealthStatus{Healthy: true}
}

func (p *startupProfileFallbackPipeline) writeReadyArtifacts(sessionID string) error {
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
		if err := os.WriteFile(filepath.Join(sessionDir, name), []byte("startup-recovery"), 0o600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(sessionDir, model.SessionFirstFrameMarkerFilename), []byte("ready"), 0o600); err != nil {
		return err
	}
	return nil
}

func TestRunExecutionLoop_PromotesSafariStartupFailureToSafariDirty(t *testing.T) {
	ctx := context.Background()
	hlsRoot := t.TempDir()
	st := store.NewMemoryStore()
	pipe := &startupProfileFallbackPipeline{hlsRoot: hlsRoot}
	orch := &Orchestrator{
		Store:               st,
		Pipeline:            pipe,
		HLSRoot:             hlsRoot,
		LiveReadySegments:   3,
		Platform:            NewTestPlatform(hlsRoot),
		PipelineStopTimeout: time.Second,
	}

	const sid = "session-safari-recovery"
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sid,
		ServiceRef: "ref:safari",
		State:      model.SessionStarting,
		Profile: model.ProfileSpec{
			Name:         profiles.ProfileSafari,
			Container:    "fmp4",
			DVRWindowSec: 2700,
		},
	}))

	session, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, session)

	handle, finalProfile, err := orch.runExecutionLoop(
		ctx,
		ctx,
		model.StartSessionEvent{
			SessionID:  sid,
			ServiceRef: "ref:safari",
			ProfileID:  profiles.ProfileSafari,
		},
		&sessionContext{
			Mode:       model.ModeLive,
			ServiceRef: "ref:safari",
		},
		session,
		time.Now(),
		zerolog.Nop(),
		func(string, model.ReasonCode) {},
		0,
	)
	require.NoError(t, err)
	require.Equal(t, ports.RunHandle("session-safari-recovery-attempt-2"), handle)
	require.Equal(t, profiles.ProfileSafariDirty, finalProfile.Name)

	updated, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, profiles.ProfileSafariDirty, updated.Profile.Name)
	require.Equal(t, "startup_recovery:safari_dirty", updated.FallbackReason)
	require.NotNil(t, updated.PlaybackTrace)
	require.Len(t, updated.PlaybackTrace.Fallbacks, 1)
	require.Equal(t, "startup_recovery", updated.PlaybackTrace.Fallbacks[0].Trigger)
	require.Equal(t, "startup_recovery:safari_dirty", updated.PlaybackTrace.Fallbacks[0].Reason)
	require.Equal(t, profiles.ProfileSafariDirty, pipe.profiles[1])
	require.Equal(t, []string{profiles.ProfileSafari, profiles.ProfileSafariDirty}, pipe.profiles)
	require.Equal(t, int32(2), pipe.startCount.Load())
	require.Equal(t, int32(1), pipe.stopCount.Load())
}
