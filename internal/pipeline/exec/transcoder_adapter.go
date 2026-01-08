// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package exec

import (
	"context"
	"fmt"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/infra/ffmpeg"
)

// transcoderAdapter adapts new infra/ffmpeg.Executor to old Transcoder interface.
// This is a compatibility shim during migration.
type transcoderAdapter struct {
	executor *ffmpeg.Executor
	lastLogs []string
}

func newTranscoderAdapter(executor *ffmpeg.Executor) *transcoderAdapter {
	return &transcoderAdapter{
		executor: executor,
		lastLogs: make([]string, 0, 100),
	}
}

// Start implements Transcoder interface
func (a *transcoderAdapter) Start(ctx context.Context, sessionID, source string, profileSpec model.ProfileSpec, startMs int64) error {
	// Old pipeline used profile-specific Start
	// New infra uses Spec-based Start via vod.Runner
	// This is a simplified adapter - real migration should use VOD subsystem
	return fmt.Errorf("transcoderAdapter.Start not fully implemented - migrate to VOD subsystem or streaming session manager")
}

// Wait implements Transcoder interface
func (a *transcoderAdapter) Wait(ctx context.Context) (model.ExitStatus, error) {
	// Old interface had Wait(), new uses Handle.Wait()
	// We don't have a handle in adapter pattern
	return model.ExitStatus{}, fmt.Errorf("transcoderAdapter.Wait not fully implemented")
}

// Stop implements Transcoder interface
func (a *transcoderAdapter) Stop(ctx context.Context) error {
	// Old interface had Stop(), new has Handle.Stop(grace, kill)
	// We don't have a handle here in adapter pattern
	return nil
}

// LastLogLines implements Transcoder interface
func (a *transcoderAdapter) LastLogLines(n int) []string {
	if n > len(a.lastLogs) {
		n = len(a.lastLogs)
	}
	return a.lastLogs[len(a.lastLogs)-n:]
}
