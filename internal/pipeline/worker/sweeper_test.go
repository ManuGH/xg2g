package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec"
	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
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
		Store:        st,
		HLSRoot:      hlsRoot,
		LeaseKeyFunc: func(e model.StartSessionEvent) string { return e.ServiceRef },
	}

	sweeper := &Sweeper{
		Orch: orch,
		Conf: SweeperConfig{
			SessionRetention: 100 * time.Millisecond,
		},
	}

	// 1. Create Expired Terminal Session
	sid := "sess-expired"
	rec := &model.SessionRecord{
		SessionID:     sid,
		State:         model.SessionStopped,
		ServiceRef:    "ref:1",
		UpdatedAtUnix: time.Now().Add(-1 * time.Second).Unix(), // Old
	}
	require.NoError(t, st.PutSession(ctx, rec))

	// Create files
	sDir := filepath.Join(hlsRoot, "sessions", sid)
	require.NoError(t, os.MkdirAll(sDir, 0750))

	// 2. Run Sweep
	sweeper.sweepStore(ctx)

	// 3. Verify Deleted
	s, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	assert.Nil(t, s, "Session should be deleted from store")

	// 4. Verify Files Gone
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
		Store:        st,
		HLSRoot:      hlsRoot,
		LeaseKeyFunc: func(e model.StartSessionEvent) string { return e.ServiceRef },
	}
	sweeper := &Sweeper{
		Orch: orch,
		Conf: SweeperConfig{
			SessionRetention: 100 * time.Millisecond,
		},
	}

	// 1. Create Orphan Directory (Old)
	orphanID := "sess-orphan"
	orphanDir := filepath.Join(hlsRoot, "sessions", orphanID)
	require.NoError(t, os.MkdirAll(orphanDir, 0750))

	// Set ModTime to past
	oldTime := time.Now().Add(-200 * time.Millisecond)
	require.NoError(t, os.Chtimes(orphanDir, oldTime, oldTime))

	// 2. Create Active Directory (Should Keep)
	activeID := "sess-active"
	activeDir := filepath.Join(hlsRoot, "sessions", activeID)
	require.NoError(t, os.MkdirAll(activeDir, 0750))
	// In Store
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID: activeID, State: model.SessionReady,
	}))
	// Also old modtime? (Maybe streaming stopped but session active?)
	require.NoError(t, os.Chtimes(activeDir, oldTime, oldTime))

	// 3. Create Recent Orphan (Should Keep - Race condition guard)
	recentID := "sess-recent"
	recentDir := filepath.Join(hlsRoot, "sessions", recentID)
	require.NoError(t, os.MkdirAll(recentDir, 0750))
	// ModTime is Now()

	// 4. Run Sweep
	sweeper.sweepFiles(ctx)

	// 5. Verify
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
		Store: st,
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

func TestSweeper_Integration(t *testing.T) {
	// Tests that Orchestrator launches sweeper
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	st := store.NewMemoryStore()
	bus := bus.NewMemoryBus()
	hlsRoot, _ := os.MkdirTemp("", "xg2g-sweep-integ")
	defer func() { _ = os.RemoveAll(hlsRoot) }()

	orch := &Orchestrator{
		Store:          st,
		Bus:            bus,
		HLSRoot:        hlsRoot,
		LeaseTTL:       1 * time.Second,
		HeartbeatEvery: 100 * time.Millisecond,
		Owner:          "worker-integ",
		TunerSlots:     []int{1},
		ExecFactory:    &exec.StubFactory{},
		LeaseKeyFunc:   func(e model.StartSessionEvent) string { return e.ServiceRef },
		Sweeper: SweeperConfig{
			Interval:         100 * time.Millisecond,
			SessionRetention: 1 * time.Nanosecond, // Avoid default override (0 -> 24h)
		},
	}

	// Session file to be swept
	sid := "integ-sweep"
	rec := &model.SessionRecord{
		SessionID: sid, State: model.SessionStopped, UpdatedAtUnix: time.Now().Add(-1 * time.Hour).Unix(),
	}
	_ = st.PutSession(ctx, rec)
	sDir := filepath.Join(hlsRoot, "sessions", sid)
	_ = os.MkdirAll(sDir, 0750)

	// Run Orchestrator
	go func() { _ = orch.Run(ctx) }()

	// Wait for cleanup
	require.Eventually(t, func() bool {
		s, _ := st.GetSession(ctx, sid)
		return s == nil
	}, 3*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		_, err := os.Stat(sDir)
		return os.IsNotExist(err)
	}, 1*time.Second, 10*time.Millisecond)
}
