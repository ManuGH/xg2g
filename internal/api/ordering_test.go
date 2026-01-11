package api

import (
	"context"
	"reflect"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/stretchr/testify/require"
)

// TestServer_PassesNonNilRootCancelToV3 provides a mechanical proof that
// the v3 sub-handler receives a valid, non-nil cancel function during initialization.
func TestServer_PassesNonNilRootCancelToV3(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir: tmpDir,
	}
	cfgMgr := config.NewManager("")

	var capturedCancel context.CancelFunc
	// Override factory to capture the passed cancel function
	factory := func(cfg config.AppConfig, mgr *config.Manager, cancel context.CancelFunc) *v3.Server {
		capturedCancel = cancel
		// Return a minimal v3 server (not fully initialized, but enough for New)
		return &v3.Server{}
	}

	s := New(cfg, cfgMgr, WithV3ServerFactory(factory))

	require.NotNil(t, s.rootCancel, "Server's rootCancel must be initialized")
	require.NotNil(t, capturedCancel, "v3 server factory must receive a non-nil cancel function")

	// Mechanical Identity Proof: Use reflect to compare function pointers
	got := reflect.ValueOf(capturedCancel).Pointer()
	want := reflect.ValueOf(s.rootCancel).Pointer()
	require.Equal(t, want, got, "New() must pass the identical s.rootCancel pointer into the v3 factory")
}
