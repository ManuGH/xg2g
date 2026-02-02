// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod/preflight"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// PreflightProvider validates source accessibility before initiating a stream.
type PreflightProvider = preflight.PreflightProvider

// RecordingStatusProvider defines the minimal interface required for DVR read operations.
type RecordingStatusProvider interface {
	GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error)
	DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error)
}

// ScanSource defines the minimal interface required for scan status.
type ScanSource interface {
	GetStatus() scan.ScanStatus
}

// ServiceStateReader defines the minimal interface required for service listing.
type ServiceStateReader interface {
	IsEnabled(id string) bool
}

// TimerReader defines the minimal interface required for timer listing.
type TimerReader interface {
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
}

// ChannelScanner abstracts the refresh/scan subsystem for testability.
type ChannelScanner interface {
	RunBackground() bool
	GetCapability(serviceRef string) (scan.Capability, bool)
}

// TimerWriter defines the minimal interface required for timer mutations.
type TimerWriter interface {
	AddTimer(ctx context.Context, sRef string, begin, end int64, name, desc string) error
	DeleteTimer(ctx context.Context, sRef string, begin, end int64) error
	UpdateTimer(ctx context.Context, oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string, enabled bool) error
}

// ReceiverControl abstracts OpenWebIF client operations for DVR timers.
// It composes Reader, Writer, and capabilities.
type ReceiverControl interface {
	TimerReader
	TimerWriter
	DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error)
}

// receiverControlFactory creates a ReceiverControl instance.
type receiverControlFactory func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl
