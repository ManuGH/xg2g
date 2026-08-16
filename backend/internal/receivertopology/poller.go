// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// OpenWebIFPoller defines the minimal OpenWebIF client interface required for continuous background synchronization.
type OpenWebIFPoller interface {
	About(ctx context.Context) (*openwebif.AboutInfo, error)
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
	GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error)
	GetCurrent(ctx context.Context) (*openwebif.CurrentInfo, error)
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

// CollectRuntimeSnapshot polls all available OpenWebIF endpoints and synthesizes a ReceiverRuntimeSnapshot with explicit evidence levels.
func CollectRuntimeSnapshot(ctx context.Context, client OpenWebIFPoller, now time.Time) ReceiverRuntimeSnapshot {
	snap := ReceiverRuntimeSnapshot{
		ObservedAt:      now,
		StandbyEvidence: EvidenceUnknown,
		StreamPresence:  StreamPresenceUnknown,
	}
	if client == nil {
		return snap
	}

	// 1. /api/statusinfo: Standby and status flags
	if status, err := client.GetStatusInfo(ctx); err == nil && status != nil {
		snap.InStandby = (status.InStandby == "true")
		snap.StandbyEvidence = EvidenceObserved
		if status.ServiceRef != "" {
			mux, err := ParseServiceRef(status.ServiceRef)
			var muxPtr *MultiplexID
			var rfPlane *RFPlane
			if err == nil {
				muxPtr = &mux
				rfPlane = mux.RFPlane
			}
			snap.HDMIPlayback = &ObservedService{
				ServiceRef:  status.ServiceRef,
				ServiceName: status.ServiceName,
				MultiplexID: muxPtr,
				RFPlane:     rfPlane,
				Evidence:    EvidenceObserved,
			}
		}
	}

	// 2. /api/getcurrent: Authoritative current live service details (PIDs, exact service ref)
	if curr, err := client.GetCurrent(ctx); err == nil && curr != nil && curr.Info.ServiceRef != "" {
		mux, err := ParseServiceRef(curr.Info.ServiceRef)
		var muxPtr *MultiplexID
		var rfPlane *RFPlane
		if err == nil {
			muxPtr = &mux
			rfPlane = mux.RFPlane
		}
		pids := make(map[string]any)
		if curr.Info.VideoPID != nil {
			pids["vpid"] = curr.Info.VideoPID
		}
		if curr.Info.AudioPID != nil {
			pids["apid"] = curr.Info.AudioPID
		}
		if curr.Info.PMTPID != nil {
			pids["pmtpid"] = curr.Info.PMTPID
		}
		snap.HDMIPlayback = &ObservedService{
			ServiceRef:  curr.Info.ServiceRef,
			ServiceName: curr.Info.ServiceName,
			MultiplexID: muxPtr,
			RFPlane:     rfPlane,
			PIDs:        pids,
			Evidence:    EvidenceObserved,
		}
	}

	// 3. /api/timerlist: Active and upcoming recordings
	if timers, err := client.GetTimers(ctx); err == nil && timers != nil {
		nowUnix := now.Unix()
		for _, t := range timers {
			if t.Disabled != 0 {
				continue
			}
			mux, err := ParseServiceRef(t.ServiceRef)
			var muxPtr *MultiplexID
			if err == nil {
				muxPtr = &mux
			}
			rec := ObservedRecording{
				TimerID:     fmt.Sprintf("%s_%s", t.ServiceRef, t.Name),
				Title:       t.Name,
				ServiceRef:  t.ServiceRef,
				MultiplexID: muxPtr,
				StartTime:   time.Unix(t.Begin, 0).UTC(),
				EndTime:     time.Unix(t.End, 0).UTC(),
				Evidence:    EvidenceObserved,
			}
			if nowUnix >= t.Begin && nowUnix <= t.End {
				rec.IsActiveNow = true
				snap.ActiveRecordings = append(snap.ActiveRecordings, rec)
			} else if nowUnix < t.Begin {
				snap.UpcomingRecordings = append(snap.UpcomingRecordings, rec)
			}
		}
	}

	// 4. /api/about: Hardware tuners and streams
	if about, err := client.About(ctx); err == nil && about != nil {
		snap.StreamPresence = ParseStreamPresence(about.Info.Streams)
		for i, t := range about.Info.Tuners {
			snap.Tuners = append(snap.Tuners, ObservedTunerSlot{
				Index:     i,
				Name:      t.Name,
				Type:      t.Type,
				RawLive:   t.Live,
				RawRec:    t.Rec,
				RawStream: t.Stream,
				Evidence:  EvidenceObserved,
			})
			if sRef := strings.TrimSpace(t.Stream); sRef != "" && snap.StreamPresence != StreamPresenceEmpty {
				mux, err := ParseServiceRef(sRef)
				var muxPtr *MultiplexID
				if err == nil {
					muxPtr = &mux
				}
				snap.ReportedStreams = append(snap.ReportedStreams, ObservedStream{
					RawRef:      sRef,
					ServiceRef:  sRef,
					MultiplexID: muxPtr,
					TunerIndex:  i,
					Evidence:    EvidenceObserved,
				})
			}
		}
	}

	return snap
}

// ExtractExternalAllocationsFromSnapshot derives external tuner allocations from an evidentiary runtime snapshot.
func ExtractExternalAllocationsFromSnapshot(
	snapshot ReceiverRuntimeSnapshot,
	topology ReceiverTopology,
	activeXG2GDemods map[DemodulatorID]bool,
) []ExternalAllocation {
	var external []ExternalAllocation

	// 1. HDMI Live TV: Only occupies RF resources when NOT in Standby!
	if !snapshot.InStandby && snapshot.HDMIPlayback != nil && snapshot.HDMIPlayback.MultiplexID != nil {
		external = append(external, ExternalAllocation{
			Source:      "hdmi_live_tv",
			MultiplexID: snapshot.HDMIPlayback.MultiplexID,
			RFPlane:     snapshot.HDMIPlayback.RFPlane,
		})
	}

	// 2. Active Local DVR Recordings
	for _, rec := range snapshot.ActiveRecordings {
		if rec.MultiplexID != nil {
			external = append(external, ExternalAllocation{
				Source:      "local_timer_dvr",
				MultiplexID: rec.MultiplexID,
				RFPlane:     rec.MultiplexID.RFPlane,
			})
		}
	}

	// 3. External Streams reported by OpenWebIF (suppressed when StreamPresence is Empty)
	if snapshot.StreamPresence != StreamPresenceEmpty {
		for _, st := range snapshot.ReportedStreams {
			demodID := DemodulatorID(fmt.Sprintf("tuner_%c", 'a'+st.TunerIndex))
			if activeXG2GDemods[demodID] {
				// Correlated with an xg2g-owned session -> skip
				continue
			}
			if st.MultiplexID != nil {
				var inputID *InputID
				if demod, ok := topology.FindDemod(demodID); ok {
					in := demod.InputID
					inputID = &in
				}
				external = append(external, ExternalAllocation{
					Source:      "external_stream_client",
					DemodID:     &demodID,
					InputID:     inputID,
					MultiplexID: st.MultiplexID,
					RFPlane:     st.MultiplexID.RFPlane,
				})
			}
		}
	}

	return external
}

// SyncOnce performs a single on-demand synchronization pass against the receiver.
func (p *ExternalSyncPoller) SyncOnce(ctx context.Context) error {
	if p.client == nil || p.service == nil {
		return nil
	}

	// 1. Elevate topology if needed from /api/about
	about, err := p.client.About(ctx)
	if err == nil && about != nil {
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
	}

	// 2. Collect multi-endpoint evidentiary snapshot
	now := time.Now()
	snapshot := CollectRuntimeSnapshot(ctx, p.client, now)
	p.service.UpdateEvidentiarySnapshot(snapshot)

	// 3. Extract external allocations using the evidentiary snapshot and update service
	activeDemods := p.service.ActiveDemods()
	external := ExtractExternalAllocationsFromSnapshot(snapshot, p.service.Topology(), activeDemods)
	p.service.UpdateExternalAllocations(external)

	// 4. Sync upcoming timer reservations
	var reservations []RecordingReservation
	for _, rec := range snapshot.UpcomingRecordings {
		if rec.MultiplexID != nil {
			reservations = append(reservations, RecordingReservation{
				ID:          rec.TimerID,
				ServiceRef:  rec.ServiceRef,
				MultiplexID: *rec.MultiplexID,
				StartTime:   rec.StartTime,
				EndTime:     rec.EndTime,
				Title:       rec.Title,
				Priority:    PriorityUpcomingRecording,
			})
		}
	}
	for _, rec := range snapshot.ActiveRecordings {
		if rec.MultiplexID != nil {
			reservations = append(reservations, RecordingReservation{
				ID:          rec.TimerID,
				ServiceRef:  rec.ServiceRef,
				MultiplexID: *rec.MultiplexID,
				StartTime:   rec.StartTime,
				EndTime:     rec.EndTime,
				Title:       rec.Title,
				Priority:    PriorityUpcomingRecording,
			})
		}
	}
	p.service.SyncTimers(reservations)

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
