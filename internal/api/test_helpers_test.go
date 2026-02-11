package api

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func mustNewServer(t testing.TB, cfg config.AppConfig, cfgMgr *config.Manager, opts ...ServerOption) *Server {
	t.Helper()
	if cfgMgr == nil {
		cfgMgr = config.NewManager("")
	}
	s, err := New(cfg, cfgMgr, opts...)
	if err != nil {
		t.Fatalf("failed to initialize api server: %v", err)
	}
	return s
}
