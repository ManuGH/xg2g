// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/rs/zerolog"
)

// App owns the long-lived runtime lifecycle (watchers, reload wiring, schedulers)
// and delegates server management to Manager.
type App struct {
	logger       zerolog.Logger
	manager      Manager
	cfgHolder    *config.ConfigHolder
	apiServer    *api.Server
	proxyOnly    bool
	reloadSignal os.Signal
}

// NewApp creates a new App orchestrator.
func NewApp(logger zerolog.Logger, manager Manager, cfgHolder *config.ConfigHolder, apiServer *api.Server, proxyOnly bool) *App {
	return &App{
		logger:       logger,
		manager:      manager,
		cfgHolder:    cfgHolder,
		apiServer:    apiServer,
		proxyOnly:    proxyOnly,
		reloadSignal: syscall.SIGHUP,
	}
}

// Run starts all owned background subsystems and blocks until ctx is cancelled or a fatal error occurs.
func (a *App) Run(ctx context.Context) error {
	if a.manager == nil {
		return ErrMissingManager
	}

	g, ctx := errgroup.WithContext(ctx)

	// Config watcher is best-effort: startup should not fail if watcher cannot be started.
	if a.cfgHolder != nil {
		if err := a.cfgHolder.StartWatcher(ctx); err != nil {
			a.logger.Warn().Err(err).Str("event", "config.watcher_start_failed").Msg("failed to start config watcher")
		}
	}

	// Reload-during-runtime wiring: ApplySnapshot on every config swap.
	if a.cfgHolder != nil && a.apiServer != nil {
		applyCh := make(chan *config.Snapshot, 1)
		a.cfgHolder.RegisterSnapshotListener(applyCh)

		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case snap := <-applyCh:
					if snap != nil {
						a.apiServer.ApplySnapshot(snap)
					}
				}
			}
		})
	}

	// SIGHUP trigger for manual reload.
	if a.cfgHolder != nil && a.reloadSignal != nil {
		g.Go(func() error {
			hupChan := make(chan os.Signal, 1)
			signal.Notify(hupChan, a.reloadSignal)
			defer signal.Stop(hupChan)

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-hupChan:
					a.logger.Info().
						Str("event", "config.reload_signal").
						Str("signal", a.reloadSignal.String()).
						Msg("received reload signal, reloading config")

					if err := a.cfgHolder.Reload(context.Background()); err != nil {
						a.logger.Warn().
							Err(err).
							Str("event", "config.reload_failed").
							Msg("config reload failed")
					}
				}
			}
		})
	}

	// DVR scheduler (owned by the daemon; stops via ctx).
	if a.apiServer != nil {
		if seriesEngine := a.apiServer.GetSeriesEngine(); seriesEngine != nil {
			sched := dvr.NewScheduler(seriesEngine)
			sched.Start(ctx)
		}
	}

	// SSDP announcer (best-effort; stops via ctx).
	if !a.proxyOnly && a.apiServer != nil {
		if hdhrSrv := a.apiServer.HDHomeRunServer(); hdhrSrv != nil {
			g.Go(func() error {
				if err := hdhrSrv.StartSSDPAnnouncer(ctx); err != nil {
					a.logger.Error().
						Err(err).
						Str("event", "ssdp.failed").
						Msg("SSDP announcer failed")
				}
				return nil
			})
		}
	}

	// Main server lifecycle.
	g.Go(func() error {
		err := a.manager.Start(ctx)
		if err != nil {
			_ = a.manager.Shutdown(context.Background())
		}
		return err
	})

	return g.Wait()
}
