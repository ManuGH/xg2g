// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	worker "github.com/ManuGH/xg2g/internal/domain/session/manager"
	"github.com/ManuGH/xg2g/internal/infra/bus"
	"github.com/ManuGH/xg2g/internal/infra/platform"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/google/uuid"
)

type v3OrchestratorFactory struct{}

func buildV3OrchestratorFactory() daemon.V3OrchestratorFactory {
	return v3OrchestratorFactory{}
}

func (v3OrchestratorFactory) Build(cfg config.AppConfig, inputs daemon.V3OrchestratorInputs) (daemon.V3Orchestrator, error) {
	if inputs.Bus == nil {
		return nil, fmt.Errorf("v3 orchestrator input bus is required")
	}
	if inputs.Store == nil {
		return nil, fmt.Errorf("v3 orchestrator input store is required")
	}
	if inputs.Pipeline == nil {
		return nil, fmt.Errorf("v3 orchestrator input pipeline is required")
	}

	host, _ := os.Hostname()
	workerOwner := fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String())

	orch := &worker.Orchestrator{
		Store:               inputs.Store,
		Bus:                 bus.NewAdapter(inputs.Bus),
		Platform:            platform.NewOSPlatform(),
		LeaseTTL:            30 * time.Second,
		HeartbeatEvery:      10 * time.Second,
		Owner:               workerOwner,
		TunerSlots:          cfg.Engine.TunerSlots,
		HLSRoot:             cfg.HLS.Root,
		PipelineStopTimeout: 5 * time.Second,
		StartConcurrency:    10,
		StopConcurrency:     10,
		Sweeper: worker.SweeperConfig{
			IdleTimeout:      cfg.Engine.IdleTimeout,
			Interval:         1 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
		OutboundPolicy: platformnet.OutboundPolicy{
			Enabled: cfg.Network.Outbound.Enabled,
			Allow: platformnet.OutboundAllowlist{
				Hosts:   append([]string(nil), cfg.Network.Outbound.Allow.Hosts...),
				CIDRs:   append([]string(nil), cfg.Network.Outbound.Allow.CIDRs...),
				Ports:   append([]int(nil), cfg.Network.Outbound.Allow.Ports...),
				Schemes: append([]string(nil), cfg.Network.Outbound.Allow.Schemes...),
			},
		},
	}
	orch.Pipeline = inputs.Pipeline
	return orch, nil
}
