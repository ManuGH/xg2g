// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
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
