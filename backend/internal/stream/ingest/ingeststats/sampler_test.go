// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ingeststats

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

// published is what the registry currently holds for one role. The metrics are
// process-global, so every assertion below is on a delta rather than an absolute.
type published struct {
	overruns      float64
	dropped       float64
	resyncSkipped float64
	lagSamples    uint64
}

func read(t *testing.T, role Role) published {
	t.Helper()

	label := string(role)
	return published{
		overruns:      counterValue(t, metrics.IngestSubscriberOverrunTotal.WithLabelValues(label)),
		dropped:       counterValue(t, metrics.IngestSubscriberDroppedBytesTotal.WithLabelValues(label)),
		resyncSkipped: counterValue(t, metrics.IngestSubscriberResyncSkippedBytesTotal.WithLabelValues(label)),
		lagSamples:    histogramCount(t, metrics.IngestSubscriberLagBytes.WithLabelValues(label)),
	}
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()

	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

func histogramCount(t *testing.T, o prometheus.Observer) uint64 {
	t.Helper()

	m, ok := o.(prometheus.Metric)
	if !ok {
		t.Fatal("lag observer is not a prometheus.Metric")
	}
	var out dto.Metric
	if err := m.Write(&out); err != nil {
		t.Fatalf("read histogram: %v", err)
	}
	return out.GetHistogram().GetSampleCount()
}

// overrunSubscriber returns a subscriber the ring has already overtaken, together
// with the recovery it performed. A real capture is used rather than filler: the
// reader only leaves the awaiting-random-access state once it finds a keyframe, so
// the stream has to carry a topology and a GOP structure it can actually index.
func overrunSubscriber(t *testing.T) (*ring.SubscriberReader, ring.SubscriberStats) {
	t.Helper()

	capture := tsfixture.Load(t, "verify_final_v3.ts")

	// A ring several times smaller than the capture, so the writer laps a
	// subscriber that has not read a single byte.
	r := ring.NewMasterRing(1024 * 1024)
	t.Cleanup(r.Close)

	reader := r.NewSubscriberReader(0)
	t.Cleanup(func() { _ = reader.Close() })

	for i := 0; i < len(capture); i += 64 * ring.TSPacketSize {
		end := i + 64*ring.TSPacketSize
		if end > len(capture) {
			end = len(capture)
		}
		if _, err := r.Push(context.Background(), capture[i:end]); err != nil {
			t.Fatalf("push capture: %v", err)
		}
	}

	if _, ok := r.LatestKeyframeOffset(); !ok {
		t.Skip("skipping: capture left no random access point in the ring")
	}

	// The first read is the whole recovery: overrun accounted, cursor moved to a
	// random access point, PAT/PMT queued in front of it.
	buf := make([]byte, 32*1024)
	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("read after overrun: %v", err)
	}

	stats := reader.Stats()
	if stats.Overruns == 0 || stats.DroppedBytes == 0 {
		t.Fatalf("no overrun was produced: %+v", stats)
	}
	if stats.ResyncSkippedBytes == 0 {
		t.Skip("skipping: recovery re-entered at the tail, leaving nothing skipped to account for")
	}
	return reader, stats
}

// The two byte counters must stay on their own series. Dropped bytes are pressure
// on the ring; skipped bytes are the correction recovery chose on top of it, and an
// exporter that added them together would report every overrun as worse than it was.
func TestSubscriberSampler_KeepsDroppedAndResyncSkippedApart(t *testing.T) {
	reader, stats := overrunSubscriber(t)

	before := read(t, RoleNativeClient)
	sampler := NewSubscriberSampler(RoleNativeClient, reader)
	sampler.Flush()
	after := read(t, RoleNativeClient)

	if got, want := after.overruns-before.overruns, float64(stats.Overruns); got != want {
		t.Fatalf("published %v overruns, ring counted %v", got, want)
	}
	if got, want := after.dropped-before.dropped, float64(stats.DroppedBytes); got != want {
		t.Fatalf("published %v dropped bytes, ring counted %v", got, want)
	}
	if got, want := after.resyncSkipped-before.resyncSkipped, float64(stats.ResyncSkippedBytes); got != want {
		t.Fatalf("published %v resync-skipped bytes, ring counted %v", got, want)
	}
	if stats.DroppedBytes == stats.ResyncSkippedBytes {
		t.Fatal("the two counters carried the same value, so this assertion proves nothing")
	}
}

// The ring counts cumulatively and Prometheus counters are added to, so a sampler
// that republished the total on every call would multiply every fault by however
// often the read loop happened to sample.
func TestSubscriberSampler_PublishesDeltasNotTotals(t *testing.T) {
	reader, stats := overrunSubscriber(t)

	sampler := NewSubscriberSampler(RoleNativeClient, reader)
	sampler.Flush()

	afterFirst := read(t, RoleNativeClient)
	for i := 0; i < 3; i++ {
		sampler.Flush()
	}
	afterRepeats := read(t, RoleNativeClient)

	if afterRepeats.dropped != afterFirst.dropped {
		t.Fatalf("re-flushing added %v dropped bytes on top of %v",
			afterRepeats.dropped-afterFirst.dropped, float64(stats.DroppedBytes))
	}
	if afterRepeats.overruns != afterFirst.overruns {
		t.Fatalf("re-flushing added %v overruns", afterRepeats.overruns-afterFirst.overruns)
	}
	if afterRepeats.resyncSkipped != afterFirst.resyncSkipped {
		t.Fatalf("re-flushing added %v resync-skipped bytes", afterRepeats.resyncSkipped-afterFirst.resyncSkipped)
	}
}

// Sample sits on the read loop, which runs thousands of times a second. It observes
// lag on a fixed interval instead of on every read, so the distribution describes
// time rather than whichever subscriber happened to read most often.
func TestSubscriberSampler_PacesLagObservation(t *testing.T) {
	reader, _ := overrunSubscriber(t)

	before := read(t, RoleVariantClient)
	sampler := NewSubscriberSampler(RoleVariantClient, reader)

	for i := 0; i < 100; i++ {
		sampler.Sample()
	}
	afterBurst := read(t, RoleVariantClient)

	if got := afterBurst.lagSamples - before.lagSamples; got != 1 {
		t.Fatalf("100 samples inside one interval produced %d observations, want 1", got)
	}

	sampler.nextAt = time.Now().Add(-time.Millisecond)
	sampler.Sample()
	if got := read(t, RoleVariantClient).lagSamples - afterBurst.lagSamples; got != 1 {
		t.Fatalf("the interval elapsed but produced %d observations, want 1", got)
	}
}

// A generation cut is the variant lifecycle working as designed. Counting it as an
// error would make a channel that legitimately changes its PMT indistinguishable
// from a transcoder that keeps crashing.
func TestRecordVariantWorkerStopped_SeparatesReasons(t *testing.T) {
	readReason := func(reason string) float64 {
		return counterValue(t, metrics.IngestVariantWorkerStoppedTotal.WithLabelValues(reason))
	}

	beforeCut := readReason(WorkerStopGenerationChange)
	beforeErr := readReason(WorkerStopError)

	RecordVariantWorkerStopped(WorkerStopGenerationChange)

	if got := readReason(WorkerStopGenerationChange) - beforeCut; got != 1 {
		t.Fatalf("generation_change moved by %v, want 1", got)
	}
	if got := readReason(WorkerStopError) - beforeErr; got != 0 {
		t.Fatalf("a generation cut moved the error counter by %v", got)
	}
}

func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("read gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

func TestTimelineSampler_NilSafety(t *testing.T) {
	// Nil reader returns nil sampler
	s := NewTimelineSampler(RoleNativeClient, nil)
	if s != nil {
		t.Fatalf("expected nil sampler from nil reader, got %v", s)
	}

	// Safe to call methods on nil receiver
	s.Sample()
	s.Flush()
}

func TestTimelineSampler_PublishesGaugesAndPaces(t *testing.T) {
	idx := timeline.NewMediaIndex()

	res := mediafacts.ParseResult{
		Coverage:               mediafacts.ParseCoverageComplete,
		ProcessedThroughOffset: 2000,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityCanonical,
			Records: []mediafacts.TimingRecord{
				{
					Type: mediafacts.TimingRecordTypeDiscontinuity,
					Discontinuity: mediafacts.DiscontinuityRecord{
						Scope:         mediafacts.DiscontinuityScopeProgram,
						Reason:        mediafacts.DiscontinuityReasonProgramIdentityChanged,
						ObservedAt:    0,
						HasEpochAfter: true,
						EpochAfter:    1,
					},
				},
				{
					Type: mediafacts.TimingRecordTypePCR,
					PCR: mediafacts.PCRPoint{
						Epoch:          1,
						PCRPID:         256,
						ObservedAt:     500,
						ExtendedPCR27m: 27_000_000,
					},
				},
				{
					Type: mediafacts.TimingRecordTypePES,
					PES: mediafacts.TimingPoint{
						Epoch:      1,
						PID:        257,
						ObservedAt: 1000,
						SubjectAt:  1000,
						HasPTS:     true,
						PTS90k:     90000,
					},
				},
				{
					Type: mediafacts.TimingRecordTypeRandomAccessPoint,
					RAP: mediafacts.TimingPoint{
						Epoch:      1,
						PID:        257,
						ObservedAt: 1000,
						SubjectAt:  1000,
						HasPTS:     true,
						PTS90k:     90000,
					},
				},
			},
		},
		Events: []mediafacts.Event{
			{
				Kind:     mediafacts.EventRandomAccessPoint,
				Offset:   1000,
				Joinable: true,
			},
		},
	}
	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("apply ingest failed: %v", err)
	}

	sampler := NewTimelineSampler(RoleNativeClient, idx)
	if sampler == nil {
		t.Fatal("expected non-nil sampler")
	}

	// Flush immediately publishes gauges
	sampler.Flush()

	lbl := string(RoleNativeClient)
	if got := gaugeValue(t, metrics.IngestTimelineBoundRAPRatio.WithLabelValues(lbl)); got != 1.0 {
		t.Fatalf("BoundRAPRatio = %v, want 1.0", got)
	}
	if got := gaugeValue(t, metrics.IngestTimelineRAPCount.WithLabelValues(lbl)); got != 1.0 {
		t.Fatalf("RAPCount = %v, want 1.0", got)
	}
	if got := gaugeValue(t, metrics.IngestTimelineEpochSpans.WithLabelValues(lbl)); got != 1.0 {
		t.Fatalf("EpochSpans = %v, want 1.0", got)
	}
	if got := gaugeValue(t, metrics.IngestTimelineTimingPoints.WithLabelValues(lbl)); got != 1.0 {
		t.Fatalf("TimingPoints = %v, want 1.0", got)
	}
	if got := gaugeValue(t, metrics.IngestTimelinePCREntries.WithLabelValues(lbl)); got != 1.0 {
		t.Fatalf("PCREntries = %v, want 1.0", got)
	}
	if got := gaugeValue(t, metrics.IngestTimelineEpochKeys.WithLabelValues(lbl)); got != 1.0 {
		t.Fatalf("EpochKeys = %v, want 1.0", got)
	}

	// Test pacing
	sampler.nextAt = time.Now().Add(time.Hour)
	// Mutate index with second RAP (unbound)
	err := idx.ApplyIngestResult(mediafacts.ParseResult{
		Coverage:               mediafacts.ParseCoverageComplete,
		ProcessedThroughOffset: 3000,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityCanonical,
		},
		Events: []mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2500, Joinable: true},
		},
	})
	if err != nil {
		t.Fatalf("second apply failed: %v", err)
	}

	// Sample() should not publish because nextAt is in the future
	sampler.Sample()
	if got := gaugeValue(t, metrics.IngestTimelineRAPCount.WithLabelValues(lbl)); got != 1.0 {
		t.Fatalf("RAPCount prematurely updated across paced interval: %v", got)
	}

	// When nextAt is past, Sample() publishes
	sampler.nextAt = time.Now().Add(-time.Millisecond)
	sampler.Sample()
	if got := gaugeValue(t, metrics.IngestTimelineRAPCount.WithLabelValues(lbl)); got != 2.0 {
		t.Fatalf("RAPCount did not update after interval elapsed: %v, want 2.0", got)
	}
	if got := gaugeValue(t, metrics.IngestTimelineBoundRAPRatio.WithLabelValues(lbl)); got < 0.49 || got > 0.51 {
		t.Fatalf("BoundRAPRatio = %v, want 0.5", got)
	}
}
