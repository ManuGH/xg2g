// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/model"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestRecordSessionStartOutcome_Busy(t *testing.T) {
	startLabels := map[string]string{
		"result":       "busy",
		"reason_class": "lease_busy",
		"profile":      "test-profile-busy",
		"mode":         "standard",
	}
	capLabels := map[string]string{
		"reason":  string(model.RLeaseBusy),
		"profile": "test-profile-busy",
		"mode":    "standard",
	}

	sessionStartsTotal.WithLabelValues("fail", "internal", "test-profile-busy", "standard")
	capacityRejectionsTotal.WithLabelValues(string(model.RLeaseBusy), "test-profile-busy", "standard")

	beforeStarts := getCounterValue(t, "xg2g_v3_session_starts_total", startLabels)
	beforeCap := getCounterValue(t, "xg2g_v3_capacity_rejections_total", capLabels)

	recordSessionStartOutcome("busy", model.RLeaseBusy, "test-profile-busy", "standard")

	afterStarts := getCounterValue(t, "xg2g_v3_session_starts_total", startLabels)
	afterCap := getCounterValue(t, "xg2g_v3_capacity_rejections_total", capLabels)

	require.Equal(t, beforeStarts, afterStarts)
	require.Equal(t, beforeCap+1, afterCap)
}

func TestRecordSessionStartOutcome_Fail(t *testing.T) {
	startLabels := map[string]string{
		"result":       "fail",
		"reason_class": "tune_failed",
		"profile":      "test-profile-fail",
		"mode":         "standard",
	}
	capLabels := map[string]string{
		"reason":  string(model.RLeaseBusy),
		"profile": "test-profile-fail",
		"mode":    "standard",
	}

	sessionStartsTotal.WithLabelValues("fail", "tune_failed", "test-profile-fail", "standard")
	capacityRejectionsTotal.WithLabelValues(string(model.RLeaseBusy), "test-profile-fail", "standard")

	beforeStarts := getCounterValue(t, "xg2g_v3_session_starts_total", startLabels)
	beforeCap := getCounterValue(t, "xg2g_v3_capacity_rejections_total", capLabels)

	recordSessionStartOutcome("fail", model.RTuneFailed, "test-profile-fail", "standard")

	afterStarts := getCounterValue(t, "xg2g_v3_session_starts_total", startLabels)
	afterCap := getCounterValue(t, "xg2g_v3_capacity_rejections_total", capLabels)

	require.Equal(t, beforeStarts+1, afterStarts)
	require.Equal(t, beforeCap, afterCap)
}

func TestRecordSessionStartOutcome_Success(t *testing.T) {
	startLabels := map[string]string{
		"result":       "success",
		"reason_class": "none",
		"profile":      "test-profile-success",
		"mode":         "standard",
	}

	sessionStartsTotal.WithLabelValues("success", "none", "test-profile-success", "standard")

	beforeStarts := getCounterValue(t, "xg2g_v3_session_starts_total", startLabels)

	recordSessionStartOutcome("success", model.RNone, "test-profile-success", "standard")

	afterStarts := getCounterValue(t, "xg2g_v3_session_starts_total", startLabels)

	require.Equal(t, beforeStarts+1, afterStarts)
}

func TestObserveTTF(t *testing.T) {
	playlistLabels := map[string]string{
		"profile": "test-profile-ttfp",
		"mode":    "standard",
	}
	segmentLabels := map[string]string{
		"profile": "test-profile-ttfs",
		"mode":    "standard",
	}

	timeToFirstPlaylist.WithLabelValues("test-profile-ttfp", "standard")
	timeToFirstSegment.WithLabelValues("test-profile-ttfs", "standard")

	beforePlaylist := getHistogramCount(t, "xg2g_v3_time_to_first_playlist_seconds", playlistLabels)
	beforeSegment := getHistogramCount(t, "xg2g_v3_time_to_first_segment_seconds", segmentLabels)

	start := time.Now().Add(-2 * time.Second)
	observeTTFP("test-profile-ttfp", "standard", start)
	observeTTFS("test-profile-ttfs", "standard", start)

	afterPlaylist := getHistogramCount(t, "xg2g_v3_time_to_first_playlist_seconds", playlistLabels)
	afterSegment := getHistogramCount(t, "xg2g_v3_time_to_first_segment_seconds", segmentLabels)

	require.Equal(t, beforePlaylist+1, afterPlaylist)
	require.Equal(t, beforeSegment+1, afterSegment)
}

func getCounterValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	mf := findMetricFamily(t, name)
	for _, m := range mf.Metric {
		if labelsMatch(m.GetLabel(), labels) {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func getHistogramCount(t *testing.T, name string, labels map[string]string) uint64 {
	t.Helper()
	mf := findMetricFamily(t, name)
	for _, m := range mf.Metric {
		if labelsMatch(m.GetLabel(), labels) {
			return m.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func findMetricFamily(t *testing.T, name string) *dto.MetricFamily {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	require.FailNow(t, "metric family not found", name)
	return nil
}

func labelsMatch(pairs []*dto.LabelPair, labels map[string]string) bool {
	if len(pairs) != len(labels) {
		return false
	}
	for _, pair := range pairs {
		if labels[pair.GetName()] != pair.GetValue() {
			return false
		}
	}
	return true
}
