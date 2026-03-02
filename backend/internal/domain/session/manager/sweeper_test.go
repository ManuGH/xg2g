package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSweeper_StoreCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := store.NewMemoryStore()
	hlsRoot, err := os.MkdirTemp("", "xg2g-sweep-store")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(hlsRoot) }()

	orch := &Orchestrator{
		Store:            st,
		Bus:              NewStubBus(),
		Pipeline:         stub.NewAdapter(),
		Platform:         NewTestPlatform(hlsRoot), // Real deletion for this test
		LeaseTTL:         30 * time.Second,
		HeartbeatEvery:   10 * time.Second,
		Owner:            "sweeper-test",
		StartConcurrency: 5,
		StopConcurrency:  5,
		HLSRoot:          hlsRoot,
		LeaseKeyFunc:     func(e model.StartSessionEvent) string { return e.ServiceRef },
	}

	sweeper := &Sweeper{
		Orch: orch,
		Conf: SweeperConfig{
			SessionRetention: 100 * time.Millisecond,
		},
	}

	sid := "sess-expired"
	rec := &model.SessionRecord{
		SessionID:     sid,
		State:         model.SessionStopped,
		ServiceRef:    "ref:1",
		UpdatedAtUnix: time.Now().Add(-1 * time.Second).Unix(),
	}
	require.NoError(t, st.PutSession(ctx, rec))

	sDir := filepath.Join(hlsRoot, "sessions", sid)
	require.NoError(t, os.MkdirAll(sDir, 0750))

	sweeper.sweepStore(ctx)

	s, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	assert.Nil(t, s, "Session should be deleted from store")

	_, err = os.Stat(sDir)
	assert.True(t, os.IsNotExist(err), "Session directory should be cleaned up")
}

func TestSweeper_FileCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := store.NewMemoryStore()
	hlsRoot, err := os.MkdirTemp("", "xg2g-sweep-files")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(hlsRoot) }()

	orch := &Orchestrator{
		Store:            st,
		Bus:              NewStubBus(),
		Pipeline:         stub.NewAdapter(),
		Platform:         NewTestPlatform(hlsRoot), // Real deletion for this test
		LeaseTTL:         30 * time.Second,
		HeartbeatEvery:   10 * time.Second,
		Owner:            "sweeper-file-test",
		StartConcurrency: 5,
		StopConcurrency:  5,
		HLSRoot:          hlsRoot,
		LeaseKeyFunc:     func(e model.StartSessionEvent) string { return e.ServiceRef },
	}
	sweeper := &Sweeper{
		Orch: orch,
		Conf: SweeperConfig{
			SessionRetention: 100 * time.Millisecond,
		},
	}

	orphanID := "sess-orphan"
	orphanDir := filepath.Join(hlsRoot, "sessions", orphanID)
	require.NoError(t, os.MkdirAll(orphanDir, 0750))

	oldTime := time.Now().Add(-200 * time.Millisecond)
	require.NoError(t, os.Chtimes(orphanDir, oldTime, oldTime))

	activeID := "sess-active"
	activeDir := filepath.Join(hlsRoot, "sessions", activeID)
	require.NoError(t, os.MkdirAll(activeDir, 0750))
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID: activeID, State: model.SessionReady,
	}))
	require.NoError(t, os.Chtimes(activeDir, oldTime, oldTime))

	recentID := "sess-recent"
	recentDir := filepath.Join(hlsRoot, "sessions", recentID)
	require.NoError(t, os.MkdirAll(recentDir, 0750))

	sweeper.sweepFiles(ctx)

	_, err = os.Stat(orphanDir)
	assert.True(t, os.IsNotExist(err), "Orphan dir should be removed")

	_, err = os.Stat(activeDir)
	assert.NoError(t, err, "Active session dir should be kept")

	_, err = os.Stat(recentDir)
	assert.NoError(t, err, "Recent dir should be kept (too young)")
}

func TestSweeper_IdleStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:     st,
	}
	sweeper := &Sweeper{
		Orch: orch,
		Conf: SweeperConfig{
			IdleTimeout: 30 * time.Second,
		},
	}

	sid := "sess-idle"
	rec := &model.SessionRecord{
		SessionID:      sid,
		State:          model.SessionReady,
		LastAccessUnix: time.Now().Add(-1 * time.Minute).Unix(),
	}
	require.NoError(t, st.PutSession(ctx, rec))

	sweeper.sweepStore(ctx)

	got, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, model.SessionStopping, got.State)
	assert.Equal(t, model.RIdleTimeout, got.Reason)
}
