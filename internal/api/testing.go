// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package api

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
)

// SetStatus sets the server status for testing purposes
// This method is only available in test builds
func (s *Server) SetStatus(status jobs.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// SetRefreshFunc sets a custom refresh function for testing
// This allows tests to stub the refresh operation
func (s *Server) SetRefreshFunc(fn func(context.Context, config.Snapshot) (*jobs.Status, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshFn = fn
}
