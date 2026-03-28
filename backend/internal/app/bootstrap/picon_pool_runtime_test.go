package bootstrap

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	"github.com/ManuGH/xg2g/internal/log"
)

type recordingManager struct {
	hooks []string
}

func (m *recordingManager) Start(context.Context) error    { return nil }
func (m *recordingManager) Shutdown(context.Context) error { return nil }
func (m *recordingManager) RegisterShutdownHook(name string, _ daemon.ShutdownHook) {
	m.hooks = append(m.hooks, name)
}

func TestContainerInitPiconPoolRegistersShutdownHook(t *testing.T) {
	tmp := t.TempDir()
	mgr := &recordingManager{}
	c := &Container{
		Logger:  log.WithComponent("test"),
		Manager: mgr,
		snapshot: config.BuildSnapshot(config.AppConfig{
			DataDir:   tmp,
			PiconBase: "http://example.com",
			Enigma2: config.Enigma2Settings{
				BaseURL: "http://receiver.local",
			},
		}, config.DefaultEnv()),
	}

	if err := c.initPiconPool(context.Background()); err != nil {
		t.Fatalf("initPiconPool returned error: %v", err)
	}
	if c.piconPool == nil {
		t.Fatal("expected picon pool to be initialized")
	}
	defer c.piconPool.Stop()

	if len(mgr.hooks) != 1 || mgr.hooks[0] != "picon_pool_stop" {
		t.Fatalf("shutdown hooks = %v, want [picon_pool_stop]", mgr.hooks)
	}
}
