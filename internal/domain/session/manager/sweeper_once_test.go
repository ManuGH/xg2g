package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infrastructure/media/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSweeper_SweepOnce_PrunesExpiredSessions proves retention pruning
func TestSweeper_SweepOnce_PrunesExpiredSessions(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	orch := &Orchestrator{
		Store:            st,
		Bus:              NewStubBus(),
		Pipeline:         stub.NewAdapter(),
		Platform:         NewStubPlatform(),
		LeaseTTL:         30 * time.Second,
		HeartbeatEvery:   10 * time.Second,
		Owner:            "sweeper-prune-test",
		StartConcurrency: 5,
		TunerSlots:       []int{1},
		StopConcurrency:  5,
		HLSRoot:          "/tmp/test",
		LeaseKeyFunc:     func(e model.StartSessionEvent) string { return e.ServiceRef },
	}

	sweeper := &Sweeper{
		Orch:      orch,
		RecoverFn: func(context.Context) error { return nil }, // noop for unit test
		Conf: SweeperConfig{
			SessionRetention: 10 * time.Second,
		},
	}

	now := time.Now()

	// Session A: terminal, old (should be pruned)
	sidExpired := "sess-expired"
	_ = st.PutSession(ctx, &model.SessionRecord{
		SessionID:     sidExpired,
		State:         model.SessionStopped,
		ServiceRef:    "ref:expired",
		UpdatedAtUnix: now.Add(-1 * time.Hour).Unix(),
		CreatedAtUnix: now.Add(-1 * time.Hour).Unix(),
	})

	// Session B: terminal, recent (should be kept)
	sidRecent := "sess-recent"
	_ = st.PutSession(ctx, &model.SessionRecord{
		SessionID:     sidRecent,
		State:         model.SessionStopped,
		ServiceRef:    "ref:recent",
		UpdatedAtUnix: now.Unix(),
		CreatedAtUnix: now.Unix(),
	})

	// Session C: non-terminal, old (should be kept - only terminal sessions pruned)
	sidActive := "sess-active"
	_ = st.PutSession(ctx, &model.SessionRecord{
		SessionID:     sidActive,
		State:         model.SessionReady,
		ServiceRef:    "ref:active",
		CreatedAtUnix: now.Add(-2 * time.Hour).Unix(),
		UpdatedAtUnix: now.Add(-2 * time.Hour).Unix(),
	})

	// Call SweepOnce (deterministic)
	sweeper.SweepOnce(ctx)

	// Assert: Expired terminal session deleted
	sExpired, _ := st.GetSession(ctx, sidExpired)
	assert.Nil(t, sExpired, "Expired terminal session should be pruned")

	// Assert: Recent terminal session kept
	sRecent, err := st.GetSession(ctx, sidRecent)
	require.NoError(t, err)
	require.NotNil(t, sRecent, "Recent terminal session should be kept")

	// Assert: Active session kept
	sActive, err := st.GetSession(ctx, sidActive)
	require.NoError(t, err)
	require.NotNil(t, sActive, "Active session should be kept regardless of age")
}

// TestSweeper_SweepOnce_RemovesFilesForPrunedSession proves file cleanup
func TestSweeper_SweepOnce_RemovesFilesForPrunedSession(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	hlsRoot := t.TempDir() // Use t.TempDir for auto cleanup

	orch := &Orchestrator{
		Store:            st,
		Bus:              NewStubBus(),
		Pipeline:         stub.NewAdapter(),
		Platform:         NewTestPlatform(hlsRoot), // Real deletion
		LeaseTTL:         30 * time.Second,
		HeartbeatEvery:   10 * time.Second,
		Owner:            "sweeper-file-test",
		StartConcurrency: 5,
		StopConcurrency:  5,
		TunerSlots:       []int{1},
		HLSRoot:          hlsRoot,
	}

	sweeper := &Sweeper{
		Orch:      orch,
		RecoverFn: func(context.Context) error { return nil }, // noop for unit test
		Conf: SweeperConfig{
			SessionRetention: 1 * time.Nanosecond, // Immediate expiry
		},
	}

	sid := "sess-file-cleanup"

	// Seed store: terminal session, old
	_ = st.PutSession(ctx, &model.SessionRecord{
		SessionID:     sid,
		State:         model.SessionStopped,
		ServiceRef:    "ref:file",
		UpdatedAtUnix: time.Now().Add(-1 * time.Hour).Unix(),
	})

	// Create session directory with a dummy file
	sDir := filepath.Join(hlsRoot, "sessions", sid)
	require.NoError(t, os.MkdirAll(sDir, 0750))
	dummyFile := filepath.Join(sDir, "index.m3u8")
	require.NoError(t, os.WriteFile(dummyFile, []byte("test"), 0600))

	// Verify file exists before sweep
	_, err := os.Stat(dummyFile)
	require.NoError(t, err, "File should exist before sweep")

	// Call SweepOnce (deterministic)
	sweeper.SweepOnce(ctx)

	// Assert: Session deleted from store
	s, _ := st.GetSession(ctx, sid)
	assert.Nil(t, s, "Session should be pruned from store")

	// Assert: Directory removed
	_, err = os.Stat(sDir)
	assert.True(t, os.IsNotExist(err), "Session directory should be removed")
}

// TestSweeper_SweepOnce_IdleTimeout proves idle session cleanup
func TestSweeper_SweepOnce_IdleTimeout(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()

	orch := &Orchestrator{
		Store:            st,
		Bus:              NewStubBus(),
		Pipeline:         stub.NewAdapter(),
		Platform:         NewStubPlatform(),
		LeaseTTL:         30 * time.Second,
		HeartbeatEvery:   10 * time.Second,
		Owner:            "sweeper-idle-test",
		StartConcurrency: 5,
		StopConcurrency:  5,
		HLSRoot:          "/tmp/test",
		LeaseKeyFunc:     func(e model.StartSessionEvent) string { return e.ServiceRef },
	}

	sweeper := &Sweeper{
		Orch:      orch,
		RecoverFn: func(context.Context) error { return nil }, // noop for unit test
		Conf: SweeperConfig{
			IdleTimeout:      30 * time.Second,
			SessionRetention: 24 * time.Hour,
		},
	}

	sid := "sess-idle"

	// Seed: READY session with old LastAccessUnix
	_ = st.PutSession(ctx, &model.SessionRecord{
		SessionID:      sid,
		State:          model.SessionReady,
		ServiceRef:     "ref:idle",
		LastAccessUnix: time.Now().Add(-1 * time.Minute).Unix(),
	})

	// Call SweepOnce
	sweeper.SweepOnce(ctx)

	// Assert: Session transitioned to STOPPING with RIdleTimeout
	got, err := st.GetSession(ctx, sid)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, model.SessionStopping, got.State, "Idle session should transition to STOPPING")
	assert.Equal(t, model.RIdleTimeout, got.Reason, "Reason should be RIdleTimeout")
}
