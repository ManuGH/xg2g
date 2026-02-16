package api

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/stretchr/testify/require"
)

// TestServer_SetRootContext_AfterStart_ReturnsError proves the "Context Invariance" requirement.
// Deliverable #4: SetRootContext must return an error if called after the server has started.
func TestServer_SetRootContext_AfterStart_ReturnsError(t *testing.T) {
	// 1. Setup
	cfg := config.AppConfig{DataDir: t.TempDir()}
	cfgMgr := config.NewManager("")

	// Mock v3 server to avoid complex wiring and ensure strict control
	mockFactory := func(cfg config.AppConfig, mgr *config.Manager, cancel context.CancelFunc) *v3.Server {
		return &v3.Server{}
	}

	s := mustNewServer(t, cfg, cfgMgr, WithV3ServerFactory(mockFactory))

	// 2. Pre-start: SetRootContext should succeed
	err := s.SetRootContext(context.Background())
	require.NoError(t, err, "SetRootContext should succeed before start")

	// 3. Start the server (signals 'started' state)
	// We use StartRecordingCacheEvicter as the proxy for "Starting Background Work"
	// In the real app, this is called by main.go in a goroutine because it blocks.
	go s.StartRecordingCacheEvicter(context.Background())

	// 4. Post-start: SetRootContext MUST fail
	// Since s.started is set atomically at the start of the function, this should be quick.
	// We use assertions.Eventually to be robust against scheduling.
	require.Eventually(t, func() bool {
		err := s.SetRootContext(context.Background())
		return err != nil && err.Error() == "cannot SetRootContext after Start"
	}, 1*time.Second, 10*time.Millisecond, "SetRootContext must eventually return error after Start called")
}
