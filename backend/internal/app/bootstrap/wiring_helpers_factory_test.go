package bootstrap

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	worker "github.com/ManuGH/xg2g/internal/domain/session/manager"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	pipebus "github.com/ManuGH/xg2g/internal/pipeline/bus"
)

func TestBuildV3OrchestratorFactory_ConfiguresHeartbeatSource(t *testing.T) {
	factory := buildV3OrchestratorFactory()
	cfg := config.AppConfig{
		Engine: config.EngineConfig{
			IdleTimeout: time.Minute,
			TunerSlots:  []int{1},
		},
		HLS: config.HLSConfig{
			Root:          "/tmp/bootstrap-hls",
			ReadySegments: 2,
		},
	}
	inputs := daemon.V3OrchestratorInputs{
		Bus:      pipebus.NewMemoryBus(),
		Store:    sessionstore.NewMemoryStore(),
		Pipeline: stub.NewAdapter(),
	}

	orchPort, err := factory.Build(cfg, inputs)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	orch, ok := orchPort.(*worker.Orchestrator)
	if !ok {
		t.Fatalf("Build() returned %T, want *worker.Orchestrator", orchPort)
	}
	hb, ok := orch.HeartbeatSource.(*worker.FSWatcherHeartbeatSource)
	if !ok {
		t.Fatalf("HeartbeatSource = %T, want *worker.FSWatcherHeartbeatSource", orch.HeartbeatSource)
	}
	if hb.HLSRoot != cfg.HLS.Root {
		t.Fatalf("HeartbeatSource.HLSRoot = %q, want %q", hb.HLSRoot, cfg.HLS.Root)
	}
}
