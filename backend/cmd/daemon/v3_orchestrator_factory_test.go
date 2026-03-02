// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	worker "github.com/ManuGH/xg2g/internal/domain/session/manager"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	pipebus "github.com/ManuGH/xg2g/internal/pipeline/bus"
)

func TestV3OrchestratorFactoryBuild_RequiresInputs(t *testing.T) {
	factory := buildV3OrchestratorFactory()

	tests := []struct {
		name   string
		inputs daemon.V3OrchestratorInputs
		want   string
	}{
		{
			name:   "missing bus",
			inputs: daemon.V3OrchestratorInputs{},
			want:   "input bus is required",
		},
		{
			name: "missing store",
			inputs: daemon.V3OrchestratorInputs{
				Bus: pipebus.NewMemoryBus(),
			},
			want: "input store is required",
		},
		{
			name: "missing pipeline",
			inputs: daemon.V3OrchestratorInputs{
				Bus:   pipebus.NewMemoryBus(),
				Store: sessionstore.NewMemoryStore(),
			},
			want: "input pipeline is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := factory.Build(config.AppConfig{}, tc.inputs)
			if err == nil {
				t.Fatal("expected Build() error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Build() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestV3OrchestratorFactoryBuild_ConfiguresWorker(t *testing.T) {
	factory := buildV3OrchestratorFactory()
	cfg := config.AppConfig{
		Engine: config.EngineConfig{
			IdleTimeout: time.Minute,
			TunerSlots:  []int{1, 2, 3},
		},
		HLS: config.HLSConfig{
			Root: "/tmp/hls",
		},
		Network: config.NetworkConfig{
			Outbound: config.OutboundConfig{
				Enabled: true,
				Allow: config.OutboundAllowlist{
					Hosts:   []string{"receiver.local"},
					CIDRs:   []string{"192.168.1.0/24"},
					Ports:   []int{80, 443},
					Schemes: []string{"http", "https"},
				},
			},
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

	if orch.Store != inputs.Store {
		t.Fatal("expected orchestrator store to match injected input")
	}
	if orch.Pipeline != inputs.Pipeline {
		t.Fatal("expected orchestrator pipeline to match injected input")
	}
	if orch.HLSRoot != cfg.HLS.Root {
		t.Fatalf("HLSRoot = %q, want %q", orch.HLSRoot, cfg.HLS.Root)
	}
	if orch.Sweeper.IdleTimeout != cfg.Engine.IdleTimeout {
		t.Fatalf("Sweeper.IdleTimeout = %v, want %v", orch.Sweeper.IdleTimeout, cfg.Engine.IdleTimeout)
	}
	if orch.Owner == "" {
		t.Fatal("Owner should be generated")
	}

	cfg.Network.Outbound.Allow.Hosts[0] = "mutated.local"
	if orch.OutboundPolicy.Allow.Hosts[0] != "receiver.local" {
		t.Fatalf("outbound host list was not copied defensively, got %v", orch.OutboundPolicy.Allow.Hosts)
	}
}
