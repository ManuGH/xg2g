package v3

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func counterValueForLabels(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, mf := range mfs {
		if mf.GetName() != metricName || mf.GetType() != dto.MetricType_COUNTER {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if metricHasLabels(metric, labels) {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func histogramCountForLabelsTracker(t *testing.T, metricName string, labels map[string]string) uint64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, mf := range mfs {
		if mf.GetName() != metricName || mf.GetType() != dto.MetricType_HISTOGRAM {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if metricHasLabels(metric, labels) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func metricHasLabels(metric *dto.Metric, labels map[string]string) bool {
	for key, want := range labels {
		found := false
		for _, lp := range metric.GetLabel() {
			if lp.GetName() == key && lp.GetValue() == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestPlaybackSessionTracker_StartIsIdempotentAndTTFFObservedOnce(t *testing.T) {
	tracker := newPlaybackSessionTracker(10 * time.Minute)
	now := time.Unix(1700000000, 0)
	tracker.nowFn = func() time.Time { return now }

	meta := playbackSessionMeta{
		SessionID:   "rec:test-id",
		Schema:      playbackSchemaRecordingLabel,
		Mode:        playbackModeHLSLabel,
		RecordingID: "test-id",
	}

	startLabels := map[string]string{
		"schema": "recording",
		"mode":   "hls",
	}
	ttffLabels := map[string]string{
		"schema":  "recording",
		"mode":    "hls",
		"outcome": "ok",
	}

	beforeStarts := counterValueForLabels(t, "xg2g_playback_start_total", startLabels)
	beforeTTFF := histogramCountForLabelsTracker(t, "xg2g_playback_ttff_seconds", ttffLabels)

	tracker.Start(meta)
	now = now.Add(2 * time.Second)
	tracker.Start(meta) // repeated start must not reset start timestamp

	afterStarts := counterValueForLabels(t, "xg2g_playback_start_total", startLabels)
	require.Equal(t, beforeStarts+1, afterStarts)

	now = now.Add(3 * time.Second)
	obs1 := tracker.MarkMediaSuccess(meta)
	require.True(t, obs1.TTFFObserved)
	require.Equal(t, 5.0, obs1.TTFFSeconds)

	now = now.Add(1 * time.Second)
	obs2 := tracker.MarkMediaSuccess(meta)
	require.False(t, obs2.TTFFObserved)

	afterTTFF := histogramCountForLabelsTracker(t, "xg2g_playback_ttff_seconds", ttffLabels)
	require.Equal(t, beforeTTFF+1, afterTTFF)

	now = now.Add(1 * time.Second)
	tracker.Start(meta)
	afterStartsAgain := counterValueForLabels(t, "xg2g_playback_start_total", startLabels)
	require.Equal(t, afterStarts, afterStartsAgain)
}

func TestClassifyPlaybackRebufferSeverity_Boundaries(t *testing.T) {
	require.Equal(t, "", classifyPlaybackRebufferSeverity(playbackRebufferMinorGap-time.Millisecond))
	require.Equal(t, "minor", classifyPlaybackRebufferSeverity(playbackRebufferMinorGap))
	require.Equal(t, "minor", classifyPlaybackRebufferSeverity(playbackRebufferMajorGap-time.Millisecond))
	require.Equal(t, "major", classifyPlaybackRebufferSeverity(playbackRebufferMajorGap))
}

func TestPlaybackSessionTracker_RebufferBoundaries_EmitExpectedSeverity(t *testing.T) {
	tracker := newPlaybackSessionTracker(10 * time.Minute)
	base := time.Unix(1700001000, 0)
	now := base
	tracker.nowFn = func() time.Time { return now }

	baseMeta := playbackSessionMeta{
		Schema:      playbackSchemaRecordingLabel,
		Mode:        playbackModeHLSLabel,
		RecordingID: "gap-test",
	}

	minorLabels := map[string]string{
		"schema":   "recording",
		"mode":     "hls",
		"severity": "minor",
	}
	majorLabels := map[string]string{
		"schema":   "recording",
		"mode":     "hls",
		"severity": "major",
	}
	beforeMinor := counterValueForLabels(t, "xg2g_playback_rebuffer_total", minorLabels)
	beforeMajor := counterValueForLabels(t, "xg2g_playback_rebuffer_total", majorLabels)

	// below minor threshold -> no event
	metaBelow := baseMeta
	metaBelow.SessionID = "rec:gap-below"
	tracker.Start(metaBelow)
	now = base.Add(1 * time.Second)
	_ = tracker.MarkMediaSuccess(metaBelow)
	now = now.Add(playbackRebufferMinorGap - time.Millisecond)
	belowMinor := tracker.MarkMediaSuccess(metaBelow)
	require.Equal(t, "", belowMinor.RebufferSeverity)

	// exactly at minor threshold -> minor
	metaMinor := baseMeta
	metaMinor.SessionID = "rec:gap-minor"
	now = base.Add(30 * time.Second)
	tracker.Start(metaMinor)
	now = now.Add(1 * time.Second)
	_ = tracker.MarkMediaSuccess(metaMinor)
	now = now.Add(playbackRebufferMinorGap)
	atMinor := tracker.MarkMediaSuccess(metaMinor)
	require.Equal(t, "minor", atMinor.RebufferSeverity)

	// exactly at major threshold -> major
	metaMajor := baseMeta
	metaMajor.SessionID = "rec:gap-major"
	now = base.Add(60 * time.Second)
	tracker.Start(metaMajor)
	now = now.Add(1 * time.Second)
	_ = tracker.MarkMediaSuccess(metaMajor)
	now = now.Add(playbackRebufferMajorGap)
	atMajor := tracker.MarkMediaSuccess(metaMajor)
	require.Equal(t, "major", atMajor.RebufferSeverity)

	afterMinor := counterValueForLabels(t, "xg2g_playback_rebuffer_total", minorLabels)
	afterMajor := counterValueForLabels(t, "xg2g_playback_rebuffer_total", majorLabels)
	require.Equal(t, beforeMinor+1, afterMinor)
	require.Equal(t, beforeMajor+1, afterMajor)
}
