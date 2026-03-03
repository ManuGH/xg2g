// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	pipebus "github.com/ManuGH/xg2g/internal/pipeline/bus"
)

type testV3Orchestrator struct{}

func (testV3Orchestrator) Run(context.Context) error { return nil }

type captureV3Factory struct {
	called bool
	cfg    config.AppConfig
	inputs V3OrchestratorInputs
	orch   V3Orchestrator
	err    error
}

func (f *captureV3Factory) Build(cfg config.AppConfig, inputs V3OrchestratorInputs) (V3Orchestrator, error) {
	f.called = true
	f.cfg = cfg
	f.inputs = inputs
	return f.orch, f.err
}

func TestBuildV3Orchestrator_MissingFactory(t *testing.T) {
	m := &manager{}
	_, err := m.buildV3Orchestrator(config.AppConfig{}, v3WorkerRuntimeDeps{})
	if !errors.Is(err, ErrMissingV3OrchestratorFactory) {
		t.Fatalf("buildV3Orchestrator() error = %v, want %v", err, ErrMissingV3OrchestratorFactory)
	}
}

func TestBuildV3Orchestrator_DelegatesToFactory(t *testing.T) {
	b := pipebus.NewMemoryBus()
	s := sessionstore.NewMemoryStore()
	p := stub.NewAdapter()
	factory := &captureV3Factory{orch: testV3Orchestrator{}}

	m := &manager{
		deps: Deps{
			V3OrchestratorFactory: factory,
		},
	}
	cfg := config.AppConfig{HLS: config.HLSConfig{Root: "/tmp/hls"}}
	deps := v3WorkerRuntimeDeps{
		bus:      b,
		store:    s,
		pipeline: p,
	}

	orch, err := m.buildV3Orchestrator(cfg, deps)
	if err != nil {
		t.Fatalf("buildV3Orchestrator() error = %v", err)
	}
	if orch == nil {
		t.Fatal("buildV3Orchestrator() returned nil orchestrator")
	}
	if !factory.called {
		t.Fatal("expected factory to be called")
	}
	if factory.inputs.Bus != b {
		t.Fatal("expected bus input to be forwarded to factory")
	}
	if factory.inputs.Store != s {
		t.Fatal("expected store input to be forwarded to factory")
	}
	if factory.inputs.Pipeline != p {
		t.Fatal("expected pipeline input to be forwarded to factory")
	}
	if factory.cfg.HLS.Root != cfg.HLS.Root {
		t.Fatalf("expected config to be forwarded, got HLS root %q", factory.cfg.HLS.Root)
	}
}

func TestBuildV3Orchestrator_FactoryMustReturnOrchestrator(t *testing.T) {
	factory := &captureV3Factory{}
	m := &manager{
		deps: Deps{
			V3OrchestratorFactory: factory,
		},
	}

	_, err := m.buildV3Orchestrator(config.AppConfig{}, v3WorkerRuntimeDeps{})
	if !errors.Is(err, ErrMissingV3Orchestrator) {
		t.Fatalf("buildV3Orchestrator() error = %v, want %v", err, ErrMissingV3Orchestrator)
	}
}
