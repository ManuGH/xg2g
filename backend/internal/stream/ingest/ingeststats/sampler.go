// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package ingeststats publishes what the shared ingest already accounts for.
//
// The ring keeps the numbers; it does not know who is reading it, and it stays
// free of any metrics dependency. The consumers know their own role and are the
// ones holding a subscriber, so the seam between the two lives here: one sampler
// per subscription, sampled on the read loop that already exists rather than by a
// goroutine that would have to be kept alive alongside it.
package ingeststats

import (
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

// Role names the consumer on the reading end of a ring subscription. The same
// overrun means something different for each of them, so none of them share a
// series.
type Role string

const (
	// RoleVariantWorker is an FFmpeg variant worker reading the master ring. Being
	// overtaken costs it its process: it is bound to one topology generation.
	RoleVariantWorker Role = "variant_worker"

	// RoleNativeClient is an HTTP client served straight from the master ring.
	RoleNativeClient Role = "native_client"

	// RoleVariantClient is an HTTP client served from a variant ring, one transcode
	// downstream of the master. Its lag is the transcoder's output backing up, not
	// the receiver's input, which is why it is not folded into RoleNativeClient.
	RoleVariantClient Role = "variant_client"
)

// Reasons a variant worker's run loop ended. A generation change is the design
// working and must never be counted as a crash.
const (
	WorkerStopGenerationChange = "generation_change"
	WorkerStopShutdown         = "shutdown"
	WorkerStopError            = "error"
)

// RecordVariantWorkerStopped counts one variant worker termination.
func RecordVariantWorkerStopped(reason string) {
	metrics.IngestVariantWorkerStoppedTotal.WithLabelValues(reason).Inc()
}

// sampleInterval paces how often a subscription is read out. The counters below
// are cumulative in the ring, so pacing costs nothing but freshness, and lag is a
// sampled distribution that a fast subscriber must not be able to dominate.
const sampleInterval = 250 * time.Millisecond

// SubscriberSampler turns one subscriber's cumulative ring accounting into
// Prometheus deltas. It is not safe for concurrent use: one sampler belongs to one
// read loop.
//
// Every method tolerates a nil receiver, so a caller that only builds a sampler on
// some paths does not have to guard each call.
type SubscriberSampler struct {
	role      Role
	reader    *ring.SubscriberReader
	published ring.SubscriberStats
	nextAt    time.Time
}

// NewSubscriberSampler binds a sampler to a subscriber. It creates the role's
// series immediately, so a healthy consumer is visibly at zero rather than absent.
func NewSubscriberSampler(role Role, reader *ring.SubscriberReader) *SubscriberSampler {
	if reader == nil {
		return nil
	}

	label := string(role)
	metrics.IngestSubscriberOverrunTotal.WithLabelValues(label)
	metrics.IngestSubscriberDroppedBytesTotal.WithLabelValues(label)
	metrics.IngestSubscriberResyncSkippedBytesTotal.WithLabelValues(label)

	return &SubscriberSampler{role: role, reader: reader}
}

// Sample publishes what has accumulated since the last one, at most every
// sampleInterval. It is meant to be called on every read.
func (s *SubscriberSampler) Sample() {
	if s == nil {
		return
	}
	now := time.Now()
	if now.Before(s.nextAt) {
		return
	}
	s.nextAt = now.Add(sampleInterval)
	s.publish(true)
}

// Flush publishes the remainder without waiting for the interval. Call it once the
// subscription has ended: an overrun immediately before a disconnect is exactly
// the one a paced sample would otherwise lose.
func (s *SubscriberSampler) Flush() {
	if s == nil {
		return
	}
	s.publish(false)
}

func (s *SubscriberSampler) publish(observeLag bool) {
	cur := s.reader.Stats()
	label := string(s.role)

	if d := cur.Overruns - s.published.Overruns; d > 0 {
		metrics.IngestSubscriberOverrunTotal.WithLabelValues(label).Add(float64(d))
	}
	if d := cur.DroppedBytes - s.published.DroppedBytes; d > 0 {
		metrics.IngestSubscriberDroppedBytesTotal.WithLabelValues(label).Add(float64(d))
	}
	if d := cur.ResyncSkippedBytes - s.published.ResyncSkippedBytes; d > 0 {
		metrics.IngestSubscriberResyncSkippedBytesTotal.WithLabelValues(label).Add(float64(d))
	}
	s.published = cur

	if observeLag {
		metrics.IngestSubscriberLagBytes.WithLabelValues(label).Observe(float64(cur.LagBytes))
	}
}

// TimelineSampler publishes the canonical timeline stats of the rings a role is
// reading. It is sampled on the same cadence (sampleInterval = 250ms) as
// SubscriberSampler.
//
// The timeline belongs to the ring, not to the subscription, and one role can be
// reading several rings at once - two viewers on two channels are two native
// clients. A gauge per role written by each subscription alone would show
// whichever ring sampled last, and would keep showing it after the stream ended.
// So every subscription registers its ring with the role, and each publication
// states the whole role: the sum over the distinct rings it is reading right
// now. When the last subscription of a role closes, the role's series are
// removed rather than left at their final value.
type TimelineSampler struct {
	role   Role
	reader timeline.TimelineReader
	nextAt time.Time
	closed bool
}

// timelineRoles is which rings each role is reading, and how many subscriptions
// read each of them. Two subscriptions on one ring share a reader, so the ring is
// counted once. The mutex also orders every gauge write, so a publication cannot
// land after the series it belongs to has been removed.
var timelineRoles = struct {
	mu      sync.Mutex
	readers map[Role]map[timeline.TimelineReader]int
}{readers: make(map[Role]map[timeline.TimelineReader]int)}

// NewTimelineSampler registers a subscription's ring with its role and publishes
// the role at once. If reader is nil (the ring keeps no canonical timeline index),
// it returns nil.
func NewTimelineSampler(role Role, reader timeline.TimelineReader) *TimelineSampler {
	if reader == nil {
		return nil
	}

	timelineRoles.mu.Lock()
	defer timelineRoles.mu.Unlock()

	rings := timelineRoles.readers[role]
	if rings == nil {
		rings = make(map[timeline.TimelineReader]int)
		timelineRoles.readers[role] = rings
	}
	rings[reader]++
	publishTimelineRoleLocked(role)

	return &TimelineSampler{role: role, reader: reader}
}

// Sample publishes the role if sampleInterval has elapsed.
func (s *TimelineSampler) Sample() {
	if s == nil || s.closed {
		return
	}
	now := time.Now()
	if now.Before(s.nextAt) {
		return
	}
	s.nextAt = now.Add(sampleInterval)

	timelineRoles.mu.Lock()
	defer timelineRoles.mu.Unlock()
	publishTimelineRoleLocked(s.role)
}

// Close withdraws the subscription's ring from its role and publishes what the
// role is still reading. Call it once the subscription has ended; calling it
// again does nothing.
func (s *TimelineSampler) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true

	timelineRoles.mu.Lock()
	defer timelineRoles.mu.Unlock()

	if rings := timelineRoles.readers[s.role]; rings != nil {
		if rings[s.reader]--; rings[s.reader] <= 0 {
			delete(rings, s.reader)
		}
		if len(rings) == 0 {
			delete(timelineRoles.readers, s.role)
		}
	}
	publishTimelineRoleLocked(s.role)
}

// publishTimelineRoleLocked states the role's timeline gauges as the sum over the
// rings it is reading. A role reading nothing has no series at all. The ratio has
// no value while no random access point is indexed, so its series is absent then
// instead of claiming every one of zero RAPs is bound.
func publishTimelineRoleLocked(role Role) {
	label := string(role)
	rings := timelineRoles.readers[role]
	if len(rings) == 0 {
		metrics.IngestTimelineBoundRAPRatio.DeleteLabelValues(label)
		metrics.IngestTimelineRAPCount.DeleteLabelValues(label)
		metrics.IngestTimelineEpochSpans.DeleteLabelValues(label)
		metrics.IngestTimelineTimingPoints.DeleteLabelValues(label)
		metrics.IngestTimelinePCREntries.DeleteLabelValues(label)
		metrics.IngestTimelineEpochKeys.DeleteLabelValues(label)
		return
	}

	var sum timeline.TimelineStats
	for reader := range rings {
		stats := reader.Stats()
		sum.TotalRAPs += stats.TotalRAPs
		sum.BoundRAPs += stats.BoundRAPs
		sum.EpochSpans += stats.EpochSpans
		sum.TimingPoints += stats.TimingPoints
		sum.PCREntries += stats.PCREntries
		sum.EpochKeys += stats.EpochKeys
	}

	if sum.TotalRAPs > 0 {
		metrics.IngestTimelineBoundRAPRatio.WithLabelValues(label).Set(float64(sum.BoundRAPs) / float64(sum.TotalRAPs))
	} else {
		metrics.IngestTimelineBoundRAPRatio.DeleteLabelValues(label)
	}
	metrics.IngestTimelineRAPCount.WithLabelValues(label).Set(float64(sum.TotalRAPs))
	metrics.IngestTimelineEpochSpans.WithLabelValues(label).Set(float64(sum.EpochSpans))
	metrics.IngestTimelineTimingPoints.WithLabelValues(label).Set(float64(sum.TimingPoints))
	metrics.IngestTimelinePCREntries.WithLabelValues(label).Set(float64(sum.PCREntries))
	metrics.IngestTimelineEpochKeys.WithLabelValues(label).Set(float64(sum.EpochKeys))
}
