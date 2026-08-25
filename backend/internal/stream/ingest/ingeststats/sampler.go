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
	"time"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
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
