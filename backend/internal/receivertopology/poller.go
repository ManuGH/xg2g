// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// OpenWebIFPoller defines the minimal OpenWebIF client interface required for continuous background synchronization.
type OpenWebIFPoller interface {
	About(ctx context.Context) (*openwebif.AboutInfo, error)
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
}

// ExternalSyncPoller continuously synchronizes external receiver tuner activity and upcoming timer reservations.
type ExternalSyncPoller struct {
	client   OpenWebIFPoller
	service  *Service
	interval time.Duration
	log      zerolog.Logger
}

// NewExternalSyncPoller creates a background poller with a configurable poll interval (default: 3 seconds).
func NewExternalSyncPoller(client OpenWebIFPoller, service *Service, interval time.Duration, logger zerolog.Logger) *ExternalSyncPoller {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return &ExternalSyncPoller{
		client:   client,
		service:  service,
		interval: interval,
		log:      logger.With().Str("component", "receivertopology.poller").Logger(),
	}
}

// SyncOnce performs a single on-demand synchronization pass against the receiver.
func (p *ExternalSyncPoller) SyncOnce(ctx context.Context) error {
	if p.client == nil || p.service == nil {
		return nil
	}

	// 1. Sync external tuner allocations from /api/about
	about, err := p.client.About(ctx)
	if err == nil && about != nil {
		// If current topology is on ConfidenceDefault, elevate to ConfidenceObserved (Audit-Only)
		if p.service.Topology().Confidence == ConfidenceDefault {
			discovered := DiscoverTopology(about)
			if err := p.service.UpdateTopologyWithPriority(discovered, false); err != nil {
				p.log.Debug().Err(err).Msg("failed to update topology to observed")
			} else {
				p.log.Info().
					Str("model", discovered.Model).
					Int("inputs", len(discovered.Inputs)).
					Int("demods", len(discovered.Demodulators)).
					Msg("elevated receiver topology from default to observed")
			}
		}

		activeDemods := p.service.ActiveDemods()
		external := ExtractExternalAllocations(about, p.service.Topology(), activeDemods)
		p.service.UpdateExternalAllocations(external)
	} else if err != nil {
		p.log.Debug().Err(err).Msg("receiver about poll failed (receiver may be standby/busy)")
	}

	// 2. Sync upcoming timer recording reservations
	timers, err := p.client.GetTimers(ctx)
	if err == nil && timers != nil {
		var reservations []RecordingReservation
		for _, t := range timers {
			if t.Disabled == 0 {
				mux, err := ParseServiceRef(t.ServiceRef)
				if err == nil {
					reservations = append(reservations, RecordingReservation{
						ID:          t.ServiceRef + "_" + t.Name,
						ServiceRef:  t.ServiceRef,
						MultiplexID: mux,
						StartTime:   time.Unix(t.Begin, 0).UTC(),
						EndTime:     time.Unix(t.End, 0).UTC(),
						Title:       t.Name,
						Priority:    PriorityUpcomingRecording,
					})
				}
			}
		}
		p.service.SyncTimers(reservations)
	}

	return nil
}

// Run executes the continuous background poll loop until ctx is cancelled.
func (p *ExternalSyncPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Initial sync immediately
	_ = p.SyncOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.SyncOnce(ctx)
		}
	}
}
