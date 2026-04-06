// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/require"
)

func TestTransitionReady_RecordsFirstFrameTimestamp(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	sid := "sess-trace-ready"
	firstFrameAt := time.Date(2026, 3, 14, 8, 45, 0, 0, time.UTC)
	hlsRoot := t.TempDir()
	markerPath := model.SessionFirstFrameMarkerPath(hlsRoot, sid)
	require.NotEmpty(t, markerPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(markerPath), 0o755))
	require.NoError(t, os.WriteFile(markerPath, []byte("ready"), 0o600))
	require.NoError(t, os.Chtimes(markerPath, firstFrameAt, firstFrameAt))

	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID: sid,
		State:     model.SessionPriming,
		Profile:   model.ProfileSpec{Name: "compatible"},
	}))

	orch := &Orchestrator{Store: st, HLSRoot: hlsRoot}
	require.NoError(t, orch.transitionReady(ctx, model.StartSessionEvent{SessionID: sid}))

	updated, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.PlaybackTrace)
	require.Equal(t, firstFrameAt.Unix(), updated.PlaybackTrace.FirstFrameAtUnix)
}

type finalizedProfilePipeline struct {
	finalized ports.ProfileSpec
}

func (p *finalizedProfilePipeline) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	return ports.RunHandle(fmt.Sprintf("%s-handle", spec.SessionID)), nil
}

func (p *finalizedProfilePipeline) Stop(ctx context.Context, handle ports.RunHandle) error {
	return nil
}

func (p *finalizedProfilePipeline) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{Healthy: true}
}

func (p *finalizedProfilePipeline) FinalizedProfile(handle ports.RunHandle) (ports.ProfileSpec, bool) {
	return p.finalized, true
}

func TestStartPipeline_RecordsEffectiveRuntimeModeFromFinalizedProfile(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	const sid = "sess-finalized-profile"

	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID: sid,
		State:     model.SessionStarting,
		Profile: model.ProfileSpec{
			Name:           "safari",
			PolicyModeHint: ports.RuntimeModeHQ25,
		},
		ContextData: map[string]string{
			model.CtxKeyClientPath: "native_hls",
		},
	}))

	pipeline := &finalizedProfilePipeline{
		finalized: ports.ProfileSpec{
			Name:                 "safari",
			PolicyModeHint:       ports.RuntimeModeHQ25,
			EffectiveRuntimeMode: ports.RuntimeModeCopyHardened,
			EffectiveModeSource:  ports.RuntimeModeSourceEnvOverride,
			TranscodeVideo:       false,
			Container:            "mpegts",
			AudioBitrateK:        192,
		},
	}
	orch := &Orchestrator{
		Store:    st,
		Pipeline: pipeline,
	}

	session, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, session)

	handle, effectiveProfile, err := orch.startPipeline(
		ctx,
		model.StartSessionEvent{SessionID: sid, ServiceRef: "ref:live", ProfileID: "compatible"},
		&sessionContext{Mode: model.ModeLive, ServiceRef: "ref:live"},
		session.Profile,
		-1,
	)
	require.NoError(t, err)
	require.NotEmpty(t, handle)
	require.Equal(t, ports.RuntimeModeCopyHardened, effectiveProfile.EffectiveRuntimeMode)

	updated, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.PlaybackTrace)
	require.Equal(t, ports.RuntimeModeHQ25, updated.PlaybackTrace.PolicyModeHint)
	require.Equal(t, ports.RuntimeModeCopyHardened, updated.PlaybackTrace.EffectiveRuntimeMode)
	require.Equal(t, ports.RuntimeModeSourceEnvOverride, updated.PlaybackTrace.EffectiveModeSource)
}
