// Copyright (c) 2026 ManuGH
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
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLifecycleSweeper builds an orchestrator whose HLS root is a real temp dir,
// so directory removal is actually observable.
func newLifecycleSweeper(t *testing.T, retention time.Duration) (*Sweeper, *store.MemoryStore, string) {
	t.Helper()

	hlsRoot := t.TempDir()
	st := store.NewMemoryStore()
	orch := &Orchestrator{
		Store:            st,
		Bus:              NewStubBus(),
		Pipeline:         stub.NewAdapter(),
		Platform:         NewTestPlatform(hlsRoot),
		LeaseTTL:         30 * time.Second,
		HeartbeatEvery:   10 * time.Second,
		Owner:            "sweeper-lifecycle-test",
		StartConcurrency: 5,
		StopConcurrency:  5,
		HLSRoot:          hlsRoot,
		LeaseKeyFunc:     func(e model.StartSessionEvent) string { return e.ServiceRef },
	}
	return &Sweeper{
		Orch: orch,
		Conf: SweeperConfig{
			Interval:         time.Minute,
			SessionRetention: 24 * time.Hour,
			FileRetention:    retention,
		},
	}, st, hlsRoot
}

func agedSessionDir(t *testing.T, hlsRoot, sid string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(hlsRoot, "sessions", sid)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "seg_000000.m4s"), []byte("payload"), 0o640))
	old := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(dir, old, old))
	return dir
}

// A session that has ended does not own its scratch any more. Before this was
// fixed, ListSessions returned terminal records too and every one of them
// shielded its directory for the full 24h store retention -- so a crashed
// session's DVR segments stayed on disk for a day.
func TestSweeper_TerminalSessionDoesNotProtectItsScratch(t *testing.T) {
	ctx := context.Background()
	sw, st, hlsRoot := newLifecycleSweeper(t, 100*time.Millisecond)

	for _, tc := range []struct {
		name  string
		state model.SessionState
		gone  bool
	}{
		{"stopped", model.SessionStopped, true},
		{"failed", model.SessionFailed, true},
		{"cancelled", model.SessionCancelled, true},
		{"ready", model.SessionReady, false},
		{"priming", model.SessionPriming, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sid := "sess-" + tc.name
			dir := agedSessionDir(t, hlsRoot, sid, time.Second)
			require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
				SessionID: sid, State: tc.state,
			}))

			sw.sweepFiles(ctx)

			_, err := os.Stat(dir)
			if tc.gone {
				assert.True(t, os.IsNotExist(err),
					"scratch of a %s session must be reclaimed", tc.state)
			} else {
				assert.NoError(t, err,
					"scratch of a live %s session must be kept", tc.state)
			}
		})
	}
}

// An unclean exit leaves a directory with no store record at all. It must be
// gone after the next reconciliation, not after an operator notices.
func TestSweeper_CrashLeftoverIsReclaimed(t *testing.T) {
	ctx := context.Background()
	sw, _, hlsRoot := newLifecycleSweeper(t, 100*time.Millisecond)

	dir := agedSessionDir(t, hlsRoot, "2885f2b8-1f62-4fec-9638-f410b4366111", time.Second)

	sw.sweepFiles(ctx)

	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "crash leftover must be reclaimed")
}

// Run() must reconcile immediately. Waiting for the first tick means every
// restart carries the previous crash's scratch for a full interval -- and if
// startup itself is refused, forever.
func TestSweeper_RunReconcilesBeforeFirstTick(t *testing.T) {
	sw, _, hlsRoot := newLifecycleSweeper(t, 100*time.Millisecond)
	// An interval far longer than the test: only a startup pass can clean this.
	sw.Conf.Interval = time.Hour

	dir := agedSessionDir(t, hlsRoot, "sess-crash-leftover", time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sw.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		_, err := os.Stat(dir)
		return os.IsNotExist(err)
	}, 3*time.Second, 20*time.Millisecond,
		"startup pass must reclaim the leftover without waiting for a tick")

	cancel()
	<-done
}

// A partially built session -- construction failed before a record was ever
// written -- must not survive as an orphan.
func TestSweeper_PartialSessionFromFailedSetupIsReclaimed(t *testing.T) {
	ctx := context.Background()
	sw, _, hlsRoot := newLifecycleSweeper(t, 100*time.Millisecond)

	dir := filepath.Join(hlsRoot, "sessions", "sess-partial")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	// Setup died after mkdir: no init segment, no playlist, no store record.
	old := time.Now().Add(-time.Second)
	require.NoError(t, os.Chtimes(dir, old, old))

	sw.sweepFiles(ctx)

	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "partial session directory must be reclaimed")
}

// The sweeper owns exactly one namespace: <HLSRoot>/sessions. Operator content
// living beside it, and anything that is not a session ID, is not its business.
func TestSweeper_LeavesForeignContentAlone(t *testing.T) {
	ctx := context.Background()
	sw, _, hlsRoot := newLifecycleSweeper(t, 100*time.Millisecond)

	aged := func(dir string) string {
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "capture.ts"), []byte("x"), 0o640))
		old := time.Now().Add(-48 * time.Hour)
		require.NoError(t, os.Chtimes(dir, old, old))
		return dir
	}

	// Operator captures sitting next to the owned namespace.
	siblingDir := aged(filepath.Join(hlsRoot, "raw_captures"))
	// Recordings the product keeps deliberately.
	recordingsDir := aged(filepath.Join(hlsRoot, "recordings"))
	// A name inside the namespace that is not a session ID.
	unsafeDir := aged(filepath.Join(hlsRoot, "sessions", "operator notes"))
	// A loose file in the namespace.
	looseFile := filepath.Join(hlsRoot, "sessions", "README.txt")
	require.NoError(t, os.WriteFile(looseFile, []byte("keep me"), 0o640))

	sw.sweepFiles(ctx)

	for _, p := range []string{siblingDir, recordingsDir, unsafeDir, looseFile} {
		_, err := os.Stat(p)
		assert.NoError(t, err, "sweeper must not touch %s", p)
	}
}

// Scratch younger than the retention window is left alone even without a store
// record: a session being constructed right now has no record yet.
func TestSweeper_YoungScratchIsKept(t *testing.T) {
	ctx := context.Background()
	sw, _, hlsRoot := newLifecycleSweeper(t, time.Hour)

	dir := agedSessionDir(t, hlsRoot, "sess-being-built", time.Second)

	sw.sweepFiles(ctx)

	_, err := os.Stat(dir)
	assert.NoError(t, err, "scratch inside the retention window must be kept")
}

// An active DVR session is allowed to be large. The invariant is about ended
// sessions, not about how much a running one may hold.
func TestSweeper_ActiveSessionKeepsItsScratchRegardlessOfSize(t *testing.T) {
	ctx := context.Background()
	sw, st, hlsRoot := newLifecycleSweeper(t, 100*time.Millisecond)

	sid := "sess-active-dvr"
	dir := agedSessionDir(t, hlsRoot, sid, time.Hour)
	for i := 0; i < 64; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "seg_"+string(rune('a'+i%26))+".m4s"),
			make([]byte, 4096), 0o640))
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID: sid, State: model.SessionReady,
	}))

	sw.sweepFiles(ctx)

	_, err := os.Stat(dir)
	assert.NoError(t, err, "a live DVR session must keep its window")
}
