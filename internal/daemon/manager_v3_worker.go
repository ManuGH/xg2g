// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/core/urlutil"
	worker "github.com/ManuGH/xg2g/internal/domain/session/manager"
	sessionports "github.com/ManuGH/xg2g/internal/domain/session/ports"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/infra/bus"
	"github.com/ManuGH/xg2g/internal/infra/platform"
	pipebus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/google/uuid"
)

type v3WorkerRuntimeDeps struct {
	bus         pipebus.Bus
	store       sessionstore.StateStore
	resumeStore resume.Store
	e2Client    *enigma2.Client
	scanManager *scan.Manager
	pipeline    sessionports.MediaPipeline
}

// startV3Worker initializes v3 bus/store runtime and launches the orchestrator.
func (m *manager) startV3Worker(ctx context.Context, errChan chan<- error) error {
	cfg := m.deps.Config
	m.logV3WorkerStart(cfg)

	runtimeDeps, err := m.snapshotV3WorkerDeps()
	if err != nil {
		return err
	}

	m.registerV3StoreCloseHooks(runtimeDeps)
	orch := m.newV3Orchestrator(cfg, runtimeDeps)

	m.registerV3Checks(&cfg, runtimeDeps.e2Client)
	m.launchV3Orchestrator(ctx, errChan, orch)

	return nil
}

func (m *manager) logV3WorkerStart(cfg config.AppConfig) {
	m.logger.Info().
		Str("mode", cfg.Engine.Mode).
		Str("store", cfg.Store.Path).
		Str("hls_root", cfg.HLS.Root).
		Str("e2_host", urlutil.SanitizeURL(cfg.Enigma2.BaseURL)).
		Msg("starting v3 worker (Phase 7A)")
}

func (m *manager) snapshotV3WorkerDeps() (v3WorkerRuntimeDeps, error) {
	runtimeDeps := v3WorkerRuntimeDeps{
		bus:         m.deps.V3Bus,
		store:       m.deps.V3Store,
		resumeStore: m.deps.ResumeStore,
		e2Client:    m.deps.E2Client,
		scanManager: m.deps.ScanManager,
		pipeline:    m.deps.MediaPipeline,
	}
	if runtimeDeps.pipeline == nil {
		return v3WorkerRuntimeDeps{}, ErrMissingMediaPipeline
	}
	return runtimeDeps, nil
}

func (m *manager) registerV3StoreCloseHooks(deps v3WorkerRuntimeDeps) {
	if c, ok := deps.store.(interface{ Close() error }); ok {
		m.RegisterShutdownHook("v3_store_close", func(ctx context.Context) error {
			return c.Close()
		})
	}
	if c, ok := deps.resumeStore.(interface{ Close() error }); ok {
		m.RegisterShutdownHook("resume_store_close", func(ctx context.Context) error {
			return c.Close()
		})
	}
	if deps.scanManager != nil {
		m.RegisterShutdownHook("scan_store_close", func(ctx context.Context) error {
			return deps.scanManager.Close()
		})
	}
}

func (m *manager) newV3Orchestrator(cfg config.AppConfig, deps v3WorkerRuntimeDeps) *worker.Orchestrator {
	host, _ := os.Hostname()
	workerOwner := fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String())

	orch := &worker.Orchestrator{
		Store:               deps.store,
		Bus:                 bus.NewAdapter(deps.bus),
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
	orch.Pipeline = deps.pipeline
	return orch
}

func (m *manager) launchV3Orchestrator(ctx context.Context, errChan chan<- error, orch *worker.Orchestrator) {
	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan error, 1)

	m.RegisterShutdownHook("v3_orchestrator_stop", func(shutdownCtx context.Context) error {
		workerCancel()
		select {
		case <-shutdownCtx.Done():
			return fmt.Errorf("timeout waiting for v3 orchestrator stop: %w", shutdownCtx.Err())
		case <-workerDone:
			return nil
		}
	})

	go func() {
		err := orch.Run(workerCtx)
		if err != nil && !errors.Is(err, context.Canceled) {
			m.logger.Error().Err(err).Msg("v3 worker orchestrator exited unexpected")
			errChan <- fmt.Errorf("v3 worker: %w", err)
		}
		workerDone <- err
		close(workerDone)
	}()
}
