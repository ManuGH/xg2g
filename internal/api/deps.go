// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"io"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/rs/zerolog"
)

// Logger defines the logging interface for API handlers
type Logger interface {
	Info() *zerolog.Event
	Warn() *zerolog.Event
	Error() *zerolog.Event
	Debug() *zerolog.Event
}

// Metrics defines the metrics recording interface
type Metrics interface {
	Inc(name string, labels ...string)
	Observe(name string, v float64, labels ...string)
}

// AuthService defines authentication and authorization interface
type AuthService interface {
	ValidateToken(ctx context.Context, token string) (principal string, err error)
	Authorize(principal, action, resource string) bool
}

// RefreshService defines the interface for triggering refresh operations
type RefreshService interface {
	Trigger(ctx context.Context, opts jobs.Options) (*jobs.Artifacts, error)
}

// Store defines the interface for accessing files in the data directory
type Store interface {
	Open(name string) (io.ReadSeeker, error)
	Stat(name string) (size int64, mod time.Time, err error)
}

// Deps holds all dependencies for the API server
type Deps struct {
	Log     Logger
	Metrics Metrics
	Auth    AuthService
	Refresh RefreshService
	Store   Store
	Config  config.AppConfig
}
