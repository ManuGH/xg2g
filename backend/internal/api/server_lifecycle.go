// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ManuGH/xg2g/internal/log"
)

// StartMonitors starts all background monitoring tasks.
func (s *Server) StartMonitors() {
	s.mu.RLock()
	v3Handler := s.v3Handler
	rootCtx := s.rootCtx
	s.mu.RUnlock()

	if v3Handler != nil && rootCtx != nil {
		v3Handler.StartMonitor(rootCtx)
	}
}

// Shutdown performs a graceful shutdown of the server.
// P9: Safety & Shutdown
func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("shutdown context is nil")
	}

	log.L().Info().Msg("shutting down server")

	// 1. Cancel root context (signals builds to stop)
	s.mu.RLock()
	rootCancel := s.rootCancel
	v3Handler := s.v3Handler
	vodManager := s.vodManager
	s.mu.RUnlock()
	if rootCancel != nil {
		rootCancel()
	}

	// 2. Stop runtime-owned workers and close resources.
	var errs []error
	if v3Handler != nil {
		if err := v3Handler.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	} else if vodManager != nil {
		// Fallback for partial initialization paths.
		if err := vodManager.ShutdownContext(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("server shutdown errors: %w", errors.Join(errs...))
	}
	return nil
}

// SetRootContext ties server lifecycle to the provided parent context.
// SetRootContext ties server lifecycle to the provided parent context.
// It replaces any existing root context and cancels the previous one.
// Returns error if called after server usage has begun.
func (s *Server) SetRootContext(ctx context.Context) error {
	if s.started.Load() {
		return fmt.Errorf("cannot SetRootContext after Start")
	}
	if ctx == nil {
		return nil
	}
	s.mu.Lock()
	if s.rootCancel != nil {
		s.rootCancel()
	}
	s.rootCtx, s.rootCancel = context.WithCancel(ctx)
	rootCtx := s.rootCtx
	v3Handler := s.v3Handler
	s.mu.Unlock()

	if v3Handler != nil {
		if err := v3Handler.SetRuntimeContext(rootCtx); err != nil {
			return fmt.Errorf("set v3 runtime context: %w", err)
		}
	}
	return nil
}

// SetShutdownFunc wires a graceful shutdown trigger (daemon-level).
// The function should cancel the daemon root context and/or invoke manager shutdown.
func (s *Server) SetShutdownFunc(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownFn = fn
}

func (s *Server) requestShutdown(ctx context.Context) error {
	s.mu.RLock()
	fn := s.shutdownFn
	s.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

// Handler returns the configured HTTP handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	return s.routes()
}
