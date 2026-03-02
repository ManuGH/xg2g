package api

import (
	"os"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func mustNewServer(t testing.TB, cfg config.AppConfig, cfgMgr *config.Manager, opts ...ServerOption) *Server {
	t.Helper()
	// Ensure decision secret is available for wireV3Subsystem (hard-fail prerequisite).
	if os.Getenv("XG2G_DECISION_SECRET") == "" {
		os.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-api-tests")
		t.Cleanup(func() { os.Unsetenv("XG2G_DECISION_SECRET") })
	}
	if cfgMgr == nil {
		cfgMgr = config.NewManager("")
	}
	s, err := New(cfg, cfgMgr, opts...)
	if err != nil {
		t.Fatalf("failed to initialize api server: %v", err)
	}
	return s
}
