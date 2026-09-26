// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Shared-ingest subscriber accounting.
//
// Every series carries a role, because the same number means something different
// depending on who fell behind. A variant worker that is overtaken loses its
// FFmpeg process a moment later; a native client that is overtaken re-enters at
// the next random access point and keeps playing. Aggregating the two would hide
// the first inside the second.
var (
	// IngestSubscriberOverrunTotal counts how often the shared ingest ring overtook
	// a subscriber. It is the event count; the bytes are counted separately, because
	// one long stall and many short ones can discard the same amount.
	IngestSubscriberOverrunTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_ingest_subscriber_overrun_total",
		Help: "Times the shared ingest ring overtook a subscriber, by consumer role",
	}, []string{"role"})

	// IngestSubscriberDroppedBytesTotal counts bytes the ring discarded before the
	// subscriber read them: pressure on the ring, i.e. a consumer that is too slow
	// or a buffer that is too small.
	IngestSubscriberDroppedBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_ingest_subscriber_dropped_bytes_total",
		Help: "Bytes the shared ingest ring discarded before a subscriber read them, by consumer role",
	}, []string{"role"})

	// IngestSubscriberResyncSkippedBytesTotal counts bytes that were still held by
	// the ring and were skipped anyway, to re-enter at a random access point.
	//
	// Deliberately not folded into IngestSubscriberDroppedBytesTotal: this is the
	// correction, not the fault, and summing the two would make every overrun look
	// worse than it was.
	IngestSubscriberResyncSkippedBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_ingest_subscriber_resync_skipped_bytes_total",
		Help: "Bytes skipped to re-enter at a random access point after an overrun, by consumer role",
	}, []string{"role"})

	// IngestSubscriberLagBytes observes how far behind the write head subscribers
	// are running.
	//
	// A histogram rather than a gauge: lag is per subscriber, and several of them
	// share a role, so a gauge would only ever show whichever one wrote last.
	// Quantiles over the sampled population answer the question a gauge was meant
	// to - how close is the fleet to being overtaken - and survive more than one
	// viewer.
	IngestSubscriberLagBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_ingest_subscriber_lag_bytes",
		Help:    "Sampled distance between the ring write head and a subscriber's read cursor, by consumer role",
		Buckets: []float64{32 << 10, 128 << 10, 512 << 10, 1 << 20, 2 << 20, 4 << 20, 8 << 20, 16 << 20},
	}, []string{"role"})

	// IngestVariantWorkerStoppedTotal counts variant worker terminations by reason.
	//
	// A worker serves exactly one upstream topology generation, so it ending on a
	// PMT change is the design working, not a crash. Keeping the reason on the
	// series is what stops a channel that changes its topology from reading as a
	// transcoder that keeps falling over.
	IngestVariantWorkerStoppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_ingest_variant_worker_stopped_total",
		Help: "Stream variant worker terminations by reason (generation_change, shutdown, error)",
	}, []string{"reason"})

	// The IngestTimeline* gauges describe the canonical timeline of the rings a
	// role is reading right now, summed over those rings. A ring read by several
	// subscriptions of one role counts once, and a role reading nothing has no
	// series (see ingeststats.TimelineSampler).
	//
	// IngestTimelineBoundRAPRatio tracks the ratio of bound RAPs to total RAPs in the canonical timeline.
	IngestTimelineBoundRAPRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_ingest_timeline_bound_rap_ratio",
		Help: "Ratio of random access points bound to canonical PES timing, over the canonical media indexes of the rings this role is reading [0..1]; absent while none are indexed",
	}, []string{"role"})

	// IngestTimelineRAPCount tracks the total number of random access points in the canonical media index.
	IngestTimelineRAPCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_ingest_timeline_rap_count",
		Help: "Random access points held in the canonical media indexes of the rings this role is reading, summed over rings",
	}, []string{"role"})

	// IngestTimelineEpochSpans tracks the number of program epoch spans held in the canonical media index.
	IngestTimelineEpochSpans = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_ingest_timeline_epoch_spans",
		Help: "Timeline epoch spans tracked in the canonical media indexes of the rings this role is reading, summed over rings",
	}, []string{"role"})

	// IngestTimelineTimingPoints tracks the number of PES timing points held in the canonical media index.
	IngestTimelineTimingPoints = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_ingest_timeline_timing_points",
		Help: "PES timing points indexed in the canonical media indexes of the rings this role is reading, summed over rings",
	}, []string{"role"})

	// IngestTimelinePCREntries tracks the number of PCR entries held in the canonical media index.
	IngestTimelinePCREntries = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_ingest_timeline_pcr_entries",
		Help: "Canonical PCR entries indexed in the canonical media indexes of the rings this role is reading, summed over rings",
	}, []string{"role"})

	// IngestTimelineEpochKeys tracks the number of epoch map keys in rapsByPTS held in the canonical media index.
	IngestTimelineEpochKeys = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_ingest_timeline_epoch_keys",
		Help: "Epoch keys in the PTS RAP indexes of the rings this role is reading, summed over rings",
	}, []string{"role"})
)
