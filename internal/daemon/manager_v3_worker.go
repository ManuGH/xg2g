// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/ManuGH/xg2g/internal/config"
	sessionports "github.com/ManuGH/xg2g/internal/domain/session/ports"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	pipebus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

type v3WorkerRuntimeDeps struct {
	bus                 pipebus.Bus
	store               sessionstore.StateStore
	resumeStore         resume.Store
	receiverHealthCheck func(context.Context) error
	pipeline            sessionports.MediaPipeline
}

// startV3Worker initializes v3 bus/store runtime and launches the orchestrator.
func (m *manager) startV3Worker(ctx context.Context, errChan chan<- error) error {
	cfg := m.deps.Config
	m.logV3WorkerStart(cfg)

	runtimeDeps, err := m.snapshotV3WorkerDeps()
	if err != nil {
		return err
	}

	orch, err := m.buildV3Orchestrator(cfg, runtimeDeps)
	if err != nil {
		return err
	}

	m.registerV3Checks(&cfg, runtimeDeps.receiverHealthCheck)
	m.launchV3Orchestrator(ctx, errChan, orch)

	return nil
}

func (m *manager) logV3WorkerStart(cfg config.AppConfig) {
	m.logger.Info().
		Str("mode", cfg.Engine.Mode).
		Str("store", cfg.Store.Path).
		Str("hls_root", cfg.HLS.Root).
		Str("e2_host", platformnet.SanitizeURL(cfg.Enigma2.BaseURL)).
		Msg("starting v3 worker (Phase 7A)")
}

func (m *manager) snapshotV3WorkerDeps() (v3WorkerRuntimeDeps, error) {
	runtimeDeps := v3WorkerRuntimeDeps{
		bus:                 m.deps.V3Bus,
		store:               m.deps.V3Store,
		resumeStore:         m.deps.ResumeStore,
		receiverHealthCheck: m.deps.ReceiverHealthCheck,
		pipeline:            m.deps.MediaPipeline,
	}
	if runtimeDeps.pipeline == nil {
		return v3WorkerRuntimeDeps{}, ErrMissingMediaPipeline
	}
	return runtimeDeps, nil
}

func (m *manager) registerV3RuntimeCloseHooks() {
	if c, ok := m.deps.V3Store.(interface{ Close() error }); ok {
		m.RegisterShutdownHook("v3_store_close", func(ctx context.Context) error {
			return c.Close()
		})
	}
	if c, ok := m.deps.ResumeStore.(interface{ Close() error }); ok {
		m.RegisterShutdownHook("resume_store_close", func(ctx context.Context) error {
			return c.Close()
		})
	}
	if m.deps.ScanManager != nil {
		m.RegisterShutdownHook("scan_store_close", func(ctx context.Context) error {
			return m.deps.ScanManager.Close()
		})
	}
}

func (m *manager) buildV3Orchestrator(cfg config.AppConfig, deps v3WorkerRuntimeDeps) (V3Orchestrator, error) {
	factory := m.deps.V3OrchestratorFactory
	if factory == nil {
		return nil, ErrMissingV3OrchestratorFactory
	}
	orch, err := factory.Build(cfg, V3OrchestratorInputs{
		Bus:      deps.bus,
		Store:    deps.store,
		Pipeline: deps.pipeline,
	})
	if err != nil {
		return nil, fmt.Errorf("build v3 orchestrator: %w", err)
	}
	if orch == nil {
		return nil, ErrMissingV3Orchestrator
	}
	return orch, nil
}

func (m *manager) launchV3Orchestrator(ctx context.Context, errChan chan<- error, orch V3Orchestrator) {
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
