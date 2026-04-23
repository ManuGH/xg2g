package capreg

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSqliteStore_RememberDeviceAndLookupCapabilities(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	metered := false
	snapshot := DeviceSnapshot{
		Identity: DeviceIdentity{
			ClientFamily:     "android_tv_native",
			ClientCapsSource: "runtime",
			DeviceType:       "tv",
			DeviceContext: &capabilities.DeviceContext{
				Brand:        "google",
				Product:      "mdarcy",
				Device:       "foster",
				Platform:     "android-tv",
				Manufacturer: "nvidia",
				Model:        "shield",
				OSName:       "android",
				OSVersion:    "14",
				SDKInt:       34,
			},
		},
		Capabilities: capabilities.PlaybackCapabilities{
			CapabilitiesVersion:  3,
			Containers:           []string{"hls", "mp4"},
			VideoCodecs:          []string{"hevc", "h264"},
			AudioCodecs:          []string{"aac", "ac3"},
			SupportsHLS:          true,
			SupportsHLSExplicit:  true,
			DeviceType:           "tv",
			RuntimeProbeUsed:     true,
			RuntimeProbeVersion:  2,
			ClientFamilyFallback: "android_tv_native",
			MaxVideo: &capabilities.MaxVideo{
				Width:  3840,
				Height: 2160,
				Fps:    60,
			},
			DeviceContext: &capabilities.DeviceContext{
				Brand:        "google",
				Product:      "mdarcy",
				Device:       "foster",
				Platform:     "android-tv",
				Manufacturer: "nvidia",
				Model:        "shield",
				OSName:       "android",
				OSVersion:    "14",
				SDKInt:       34,
			},
		},
		Network: &capabilities.NetworkContext{
			Kind:         "ethernet",
			DownlinkKbps: 940000,
			Metered:      &metered,
		},
		UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}

	require.NoError(t, store.RememberDevice(context.Background(), snapshot))

	got, ok, err := store.LookupCapabilities(context.Background(), snapshot.Identity)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"h264", "hevc"}, got.VideoCodecs)
	require.NotNil(t, got.MaxVideo)
	assert.Equal(t, 3840, got.MaxVideo.Width)
	assert.Equal(t, "android_tv_native", got.ClientFamilyFallback)
	require.NotNil(t, got.DeviceContext)
	assert.Equal(t, "google", got.DeviceContext.Brand)
	assert.Equal(t, "mdarcy", got.DeviceContext.Product)
	assert.Equal(t, "foster", got.DeviceContext.Device)
	assert.Equal(t, "shield", got.DeviceContext.Model)
}

func TestSqliteStore_RememberHostAndRecordObservation(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hostSnapshot := HostSnapshot{
		Identity: HostIdentity{
			Hostname:     "xg2g-host",
			OSName:       "linux",
			OSVersion:    "6.9",
			Architecture: "amd64",
		},
		Runtime: playbackprofile.HostRuntimeSnapshot{
			Capabilities: playbackprofile.ServerTranscodeCapabilities{
				FFmpegAvailable:    true,
				HLSAvailable:       true,
				HasVAAPI:           true,
				VAAPIReady:         true,
				HardwareVideoCodec: []string{"h264", "hevc", "av1"},
			},
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "hevc", Verified: true, AutoEligible: true, ProbeElapsedMS: 40},
		},
		UpdatedAt: time.Unix(1_700_000_100, 0).UTC(),
	}
	require.NoError(t, store.RememberHost(context.Background(), hostSnapshot))

	sourceSnapshot := SourceSnapshot{
		SubjectKind:       "live",
		Origin:            "live_scan",
		Container:         "ts",
		VideoCodec:        "hevc",
		AudioCodec:        "ac3",
		BitrateConfidence: "high",
		BitrateBucket:     "9m_18m",
		Width:             3840,
		Height:            2160,
		FPS:               25,
		SignalFPS:         50,
		Interlaced:        true,
		ProblemFlags:      []string{"interlaced"},
		ReceiverContext: &ReceiverContext{
			Platform:            "enigma2",
			Brand:               "vuplus",
			Model:               "uno4kse",
			OSName:              "openatv",
			OSVersion:           "7.4",
			KernelVersion:       "6.1.0",
			EnigmaVersion:       "2026-03-30",
			WebInterfaceVersion: "1.5.2",
		},
		UpdatedAt: time.Unix(1_700_000_150, 0).UTC(),
	}
	require.NoError(t, store.RememberSource(context.Background(), sourceSnapshot))

	metered := false
	observation := PlaybackObservation{
		ObservedAt:         time.Unix(1_700_000_200, 0).UTC(),
		RequestID:          "req-1",
		ObservationKind:    "decision",
		Outcome:            "predicted",
		SourceRef:          "1:0:19:EF75:3F9:1:C00000:0:0:0",
		SourceFingerprint:  sourceSnapshot.Fingerprint(),
		SubjectKind:        "live",
		RequestedIntent:    "quality",
		ResolvedIntent:     "quality",
		Mode:               "transcode",
		SelectedContainer:  "fmp4",
		SelectedVideoCodec: "hevc",
		SelectedAudioCodec: "aac",
		SourceWidth:        1920,
		SourceHeight:       1080,
		SourceFPS:          25,
		HostFingerprint:    hostSnapshot.Identity.Fingerprint(),
		DeviceFingerprint:  "device-fingerprint",
		ClientCapsHash:     "caps-hash",
		Network: &capabilities.NetworkContext{
			Kind:         "ethernet",
			DownlinkKbps: 940000,
			Metered:      &metered,
		},
	}
	require.NoError(t, store.RecordObservation(context.Background(), observation))

	linked, ok, err := store.LookupDecisionObservation(context.Background(), "req-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "decision", linked.ObservationKind)
	assert.Equal(t, "predicted", linked.Outcome)
	assert.Equal(t, "transcode", linked.Mode)
	assert.Equal(t, sourceSnapshot.Fingerprint(), linked.SourceFingerprint)

	var hostCount int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(*) FROM capability_hosts`).Scan(&hostCount))
	assert.Equal(t, 1, hostCount)

	var sourceCount int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(*) FROM capability_sources`).Scan(&sourceCount))
	assert.Equal(t, 1, sourceCount)

	var observationKind, outcome, mode, selectedCodec, networkKind, sourceFingerprint, receiverContextJSON, bitrateConfidence, bitrateBucket string
	var signalFPS float64
	require.NoError(t, store.DB.QueryRow(`
		SELECT
			o.observation_kind,
			o.outcome,
			o.mode,
			o.selected_video_codec,
			o.network_kind,
			o.source_fingerprint,
			COALESCE(s.receiver_context_json, ''),
			COALESCE(s.bitrate_confidence, ''),
			COALESCE(s.bitrate_bucket, ''),
			COALESCE(s.signal_fps, 0)
		FROM capability_observations o
		LEFT JOIN capability_sources s ON s.source_fingerprint = o.source_fingerprint
		ORDER BY o.id DESC
		LIMIT 1
	`).Scan(&observationKind, &outcome, &mode, &selectedCodec, &networkKind, &sourceFingerprint, &receiverContextJSON, &bitrateConfidence, &bitrateBucket, &signalFPS))
	assert.Equal(t, "decision", observationKind)
	assert.Equal(t, "predicted", outcome)
	assert.Equal(t, "transcode", mode)
	assert.Equal(t, "hevc", selectedCodec)
	assert.Equal(t, "ethernet", networkKind)
	assert.Equal(t, sourceSnapshot.Fingerprint(), sourceFingerprint)
	assert.Equal(t, "high", bitrateConfidence)
	assert.Equal(t, "9m_18m", bitrateBucket)
	assert.Equal(t, 50.0, signalFPS)
	assert.Contains(t, receiverContextJSON, `"osName":"openatv"`)
	assert.Contains(t, receiverContextJSON, `"osVersion":"7.4"`)
}

func TestSqliteStore_LookupRecentFeedbackSummary(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sourceFingerprint := "source-fp"
	deviceFingerprint := "device-fp"
	hostFingerprint := "host-fp"

	observations := []PlaybackObservation{
		{
			ObservedAt:        time.Unix(1_700_001_300, 0).UTC(),
			RequestID:         "req-newest-fail",
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      4,
		},
		{
			ObservedAt:        time.Unix(1_700_001_200, 0).UTC(),
			RequestID:         "req-older-fail",
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      3,
		},
		{
			ObservedAt:        time.Unix(1_700_001_100, 0).UTC(),
			RequestID:         "req-started",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
		{
			ObservedAt:        time.Unix(1_700_001_000, 0).UTC(),
			RequestID:         "req-decision",
			ObservationKind:   "decision",
			Outcome:           "predicted",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
		},
	}
	for _, observation := range observations {
		require.NoError(t, store.RecordObservation(context.Background(), observation))
	}

	summary, ok, err := store.LookupRecentFeedbackSummary(context.Background(), FeedbackSummaryQuery{
		SubjectKind:       "live",
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		Since:             time.Unix(1_700_000_900, 0).UTC(),
		Limit:             8,
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, time.Unix(1_700_001_300, 0).UTC(), summary.LastObservedAt)
	assert.Equal(t, 3, summary.SampleCount)
	assert.Equal(t, 1, summary.StartedCount)
	assert.Equal(t, 2, summary.FailedCount)
	assert.Equal(t, 0, summary.ConsecutiveWarnings)
	assert.Equal(t, 0, summary.ConsecutiveBufferWarnings)
	assert.Equal(t, 0, summary.ConsecutiveDecodeWarnings)
	assert.Equal(t, 0, summary.ConsecutiveNetworkWarnings)
	assert.Equal(t, 2, summary.ConsecutiveFailures)
	assert.Equal(t, 1, summary.ConsecutiveDecodeFailures)
	assert.Equal(t, 1, summary.ConsecutiveStallFailures)
	assert.Equal(t, 1, summary.PriorStartedStreak)
}

func TestSqliteStore_LookupRecentFeedbackObservations(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sourceFingerprint := "source-observation-fp"
	deviceFingerprint := "device-observation-fp"
	hostFingerprint := "host-observation-fp"

	observations := []PlaybackObservation{
		{
			ObservedAt:        time.Unix(1_700_001_300, 0).UTC(),
			RequestID:         "req-newest-warning",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      101,
		},
		{
			ObservedAt:        time.Unix(1_700_001_200, 0).UTC(),
			RequestID:         "req-middle-started",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
		{
			ObservedAt:        time.Unix(1_700_001_100, 0).UTC(),
			RequestID:         "req-oldest-failed",
			ObservationKind:   "feedback",
			Outcome:           "failed",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "error",
			FeedbackCode:      3,
		},
	}
	for _, observation := range observations {
		require.NoError(t, store.RecordObservation(context.Background(), observation))
	}

	got, err := store.LookupRecentFeedbackObservations(context.Background(), FeedbackSummaryQuery{
		SubjectKind:       "live",
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		Since:             time.Unix(1_700_001_000, 0).UTC(),
		Limit:             2,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "req-newest-warning", got[0].RequestID)
	assert.Equal(t, "req-middle-started", got[1].RequestID)
}

func TestSqliteStore_RememberAndLookupPlaybackPolicyState(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Unix(1_700_010_000, 0).UTC()
	state := PlaybackPolicyState{
		SubjectKind:       "live",
		SourceFingerprint: "source-policy-fp",
		DeviceFingerprint: "device-policy-fp",
		HostFingerprint:   "host-policy-fp",
		MaxQualityRung:    playbackprofile.RungCompatibleVideoH264CRF23,
		Confidence: runtimepolicy.ConfidenceSnapshot{
			Score:              -22,
			State:              runtimepolicy.ConfidenceRecovery,
			WindowCount:        3,
			CooldownUntil:      now.Add(20 * time.Second),
			ProbeSuccessStreak: 2,
			LastProbeEventAt:   now.Add(-15 * time.Second),
			PolicyConstraints:  []string{"cooldown_active", "no_probe_up"},
			Reasons:            []string{"network_recently_unstable", "probe_recently_confirmed"},
		},
		UpdatedAt: now,
	}

	require.NoError(t, store.RememberPlaybackPolicyState(context.Background(), state))

	got, ok, err := store.LookupPlaybackPolicyState(context.Background(), PlaybackPolicyStateQuery{
		SubjectKind:       "live",
		SourceFingerprint: "source-policy-fp",
		DeviceFingerprint: "device-policy-fp",
		HostFingerprint:   "host-policy-fp",
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, playbackprofile.RungCompatibleVideoH264CRF23, got.MaxQualityRung)
	assert.Equal(t, -22, got.Confidence.Score)
	assert.Equal(t, runtimepolicy.ConfidenceRecovery, got.Confidence.State)
	assert.Equal(t, 3, got.Confidence.WindowCount)
	assert.Equal(t, now.Add(20*time.Second), got.Confidence.CooldownUntil)
	assert.Equal(t, 2, got.Confidence.ProbeSuccessStreak)
	assert.Equal(t, now.Add(-15*time.Second), got.Confidence.LastProbeEventAt)
	assert.Equal(t, now, got.UpdatedAt)
}

func TestSqliteStore_LookupRecentFeedbackSummary_TracksConsecutiveBufferWarnings(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sourceFingerprint := "source-buffer-warning"
	deviceFingerprint := "device-buffer-warning"
	hostFingerprint := "host-buffer-warning"

	observations := []PlaybackObservation{
		{
			ObservedAt:        time.Unix(1_700_002_300, 0).UTC(),
			RequestID:         "req-warning-newest",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      102,
		},
		{
			ObservedAt:        time.Unix(1_700_002_200, 0).UTC(),
			RequestID:         "req-warning-middle",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      101,
		},
		{
			ObservedAt:        time.Unix(1_700_002_100, 0).UTC(),
			RequestID:         "req-warning-oldest",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      101,
		},
		{
			ObservedAt:        time.Unix(1_700_002_000, 0).UTC(),
			RequestID:         "req-started-before-warning",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
	}
	for _, observation := range observations {
		require.NoError(t, store.RecordObservation(context.Background(), observation))
	}

	summary, ok, err := store.LookupRecentFeedbackSummary(context.Background(), FeedbackSummaryQuery{
		SubjectKind:       "live",
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		Since:             time.Unix(1_700_001_900, 0).UTC(),
		Limit:             8,
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 4, summary.SampleCount)
	assert.Equal(t, 1, summary.StartedCount)
	assert.Equal(t, 3, summary.WarningCount)
	assert.Equal(t, 3, summary.ConsecutiveWarnings)
	assert.Equal(t, 3, summary.ConsecutiveBufferWarnings)
	assert.Equal(t, 0, summary.ConsecutiveDecodeWarnings)
	assert.Equal(t, 0, summary.ConsecutiveNetworkWarnings)
	assert.Equal(t, 0, summary.ConsecutiveFailures)
	assert.Equal(t, 0, summary.PriorStartedStreak)
}

func TestSqliteStore_LookupRecentFeedbackSummary_TracksConsecutiveDecodeWarnings(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sourceFingerprint := "source-decode-warning"
	deviceFingerprint := "device-decode-warning"
	hostFingerprint := "host-decode-warning"

	observations := []PlaybackObservation{
		{
			ObservedAt:        time.Unix(1_700_003_300, 0).UTC(),
			RequestID:         "req-decode-warning-newest",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      103,
		},
		{
			ObservedAt:        time.Unix(1_700_003_200, 0).UTC(),
			RequestID:         "req-decode-warning-older",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      103,
		},
		{
			ObservedAt:        time.Unix(1_700_003_100, 0).UTC(),
			RequestID:         "req-started-before-decode-warning",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
	}
	for _, observation := range observations {
		require.NoError(t, store.RecordObservation(context.Background(), observation))
	}

	summary, ok, err := store.LookupRecentFeedbackSummary(context.Background(), FeedbackSummaryQuery{
		SubjectKind:       "live",
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		Since:             time.Unix(1_700_003_000, 0).UTC(),
		Limit:             8,
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 3, summary.SampleCount)
	assert.Equal(t, 1, summary.StartedCount)
	assert.Equal(t, 2, summary.WarningCount)
	assert.Equal(t, 2, summary.ConsecutiveWarnings)
	assert.Equal(t, 0, summary.ConsecutiveBufferWarnings)
	assert.Equal(t, 2, summary.ConsecutiveDecodeWarnings)
	assert.Equal(t, 0, summary.ConsecutiveNetworkWarnings)
	assert.Equal(t, 0, summary.ConsecutiveFailures)
	assert.Equal(t, 0, summary.PriorStartedStreak)
}

func TestSqliteStore_LookupRecentFeedbackSummary_TracksConsecutiveNetworkWarnings(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sourceFingerprint := "source-network-warning"
	deviceFingerprint := "device-network-warning"
	hostFingerprint := "host-network-warning"

	observations := []PlaybackObservation{
		{
			ObservedAt:        time.Unix(1_700_004_300, 0).UTC(),
			RequestID:         "req-network-warning-newest",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      104,
		},
		{
			ObservedAt:        time.Unix(1_700_004_200, 0).UTC(),
			RequestID:         "req-network-warning-middle",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      104,
		},
		{
			ObservedAt:        time.Unix(1_700_004_100, 0).UTC(),
			RequestID:         "req-network-warning-oldest",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      104,
		},
		{
			ObservedAt:        time.Unix(1_700_004_000, 0).UTC(),
			RequestID:         "req-started-before-network-warning",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
	}
	for _, observation := range observations {
		require.NoError(t, store.RecordObservation(context.Background(), observation))
	}

	summary, ok, err := store.LookupRecentFeedbackSummary(context.Background(), FeedbackSummaryQuery{
		SubjectKind:       "live",
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		Since:             time.Unix(1_700_003_900, 0).UTC(),
		Limit:             8,
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 4, summary.SampleCount)
	assert.Equal(t, 1, summary.StartedCount)
	assert.Equal(t, 3, summary.WarningCount)
	assert.Equal(t, 3, summary.ConsecutiveWarnings)
	assert.Equal(t, 0, summary.ConsecutiveBufferWarnings)
	assert.Equal(t, 0, summary.ConsecutiveDecodeWarnings)
	assert.Equal(t, 3, summary.ConsecutiveNetworkWarnings)
	assert.Equal(t, 0, summary.ConsecutiveFailures)
	assert.Equal(t, 0, summary.PriorStartedStreak)
	assert.Equal(t, 0, summary.PriorRecoveredStartStreak)
}

func TestSqliteStore_LookupRecentFeedbackSummary_TracksPriorRecoveredStartStreak(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capability_registry.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sourceFingerprint := "source-recovered-warning"
	deviceFingerprint := "device-recovered-warning"
	hostFingerprint := "host-recovered-warning"

	observations := []PlaybackObservation{
		{
			ObservedAt:        time.Unix(1_700_005_300, 0).UTC(),
			RequestID:         "req-recovered-warning-newest",
			ObservationKind:   "feedback",
			Outcome:           "warning",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "warning",
			FeedbackCode:      101,
		},
		{
			ObservedAt:        time.Unix(1_700_005_200, 0).UTC(),
			RequestID:         "req-recovered-start",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      211,
		},
		{
			ObservedAt:        time.Unix(1_700_005_150, 0).UTC(),
			RequestID:         "req-recovered-start-second",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      211,
		},
		{
			ObservedAt:        time.Unix(1_700_005_100, 0).UTC(),
			RequestID:         "req-started-before-recovery",
			ObservationKind:   "feedback",
			Outcome:           "started",
			SourceFingerprint: sourceFingerprint,
			SubjectKind:       "live",
			HostFingerprint:   hostFingerprint,
			DeviceFingerprint: deviceFingerprint,
			FeedbackEvent:     "info",
			FeedbackCode:      200,
		},
	}
	for _, observation := range observations {
		require.NoError(t, store.RecordObservation(context.Background(), observation))
	}

	summary, ok, err := store.LookupRecentFeedbackSummary(context.Background(), FeedbackSummaryQuery{
		SubjectKind:       "live",
		SourceFingerprint: sourceFingerprint,
		DeviceFingerprint: deviceFingerprint,
		HostFingerprint:   hostFingerprint,
		Since:             time.Unix(1_700_005_000, 0).UTC(),
		Limit:             8,
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 4, summary.SampleCount)
	assert.Equal(t, 3, summary.StartedCount)
	assert.Equal(t, 1, summary.WarningCount)
	assert.Equal(t, 1, summary.ConsecutiveWarnings)
	assert.Equal(t, 1, summary.ConsecutiveBufferWarnings)
	assert.Equal(t, 0, summary.ConsecutiveDecodeWarnings)
	assert.Equal(t, 0, summary.ConsecutiveNetworkWarnings)
	assert.Equal(t, 2, summary.PriorRecoveredStartStreak)
	assert.Equal(t, 211, summary.PriorRecoveryStartCode)
	assert.Equal(t, 0, summary.ConsecutiveFailures)
	assert.Equal(t, 0, summary.PriorStartedStreak)
}

func TestSqliteStore_MigratesV1ObservationSchemaToLatest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "capability_registry.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	require.NoError(t, err)

	identity := DeviceIdentity{
		ClientFamily:     "android_tv_native",
		ClientCapsSource: "runtime",
		DeviceType:       "tv",
		DeviceContext: &capabilities.DeviceContext{
			Brand:        "Google",
			Product:      "darcy",
			Device:       "foster",
			Platform:     "android-tv",
			Manufacturer: "NVIDIA",
			Model:        "Shield",
			OSName:       "Android",
			OSVersion:    "14",
			SDKInt:       34,
		},
	}
	legacyCaps := capabilities.PlaybackCapabilities{
		CapabilitiesVersion: 3,
		Containers:          []string{"mp4", "hls"},
		VideoCodecs:         []string{"hevc", "h264"},
		AudioCodecs:         []string{"ac3", "aac"},
		SupportsHLS:         true,
		DeviceType:          "tv",
	}
	capsJSON, err := json.Marshal(legacyCaps)
	require.NoError(t, err)

	_, err = db.Exec(`
	CREATE TABLE capability_hosts (
		host_fingerprint TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		os_name TEXT NOT NULL,
		os_version TEXT NOT NULL,
		architecture TEXT NOT NULL,
		runtime_json TEXT NOT NULL,
		encoder_caps_json TEXT NOT NULL,
		updated_at_ms INTEGER NOT NULL
	);
	CREATE TABLE capability_devices (
		device_fingerprint TEXT PRIMARY KEY,
		client_family TEXT NOT NULL,
		client_caps_source TEXT NOT NULL,
		device_type TEXT NOT NULL,
		platform TEXT NOT NULL,
		manufacturer TEXT NOT NULL,
		model TEXT NOT NULL,
		os_name TEXT NOT NULL,
		os_version TEXT NOT NULL,
		sdk_int INTEGER NOT NULL,
		capabilities_json TEXT NOT NULL,
		capabilities_hash TEXT NOT NULL,
		network_json TEXT,
		updated_at_ms INTEGER NOT NULL
	);
	CREATE TABLE capability_observations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		observed_at_ms INTEGER NOT NULL,
		request_id TEXT NOT NULL,
		source_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		resolved_intent TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		source_width INTEGER NOT NULL,
		source_height INTEGER NOT NULL,
		source_fps REAL NOT NULL,
		host_fingerprint TEXT NOT NULL,
		device_fingerprint TEXT NOT NULL,
		client_caps_hash TEXT NOT NULL,
		network_kind TEXT NOT NULL,
		network_metered INTEGER,
		network_downlink_kbps INTEGER NOT NULL
	);
	INSERT INTO capability_devices(
		device_fingerprint, client_family, client_caps_source, device_type, platform, manufacturer, model,
		os_name, os_version, sdk_int, capabilities_json, capabilities_hash, network_json, updated_at_ms
	) VALUES (?, 'android_tv_native', 'runtime', 'tv', 'android-tv', 'nvidia', 'shield', 'android', '14', 34, ?, 'legacy-hash', NULL, 1700000000000);
	INSERT INTO capability_observations(
		observed_at_ms, request_id, source_ref, subject_kind, requested_intent, resolved_intent, mode,
		selected_container, selected_video_codec, selected_audio_codec, source_width, source_height, source_fps,
		host_fingerprint, device_fingerprint, client_caps_hash, network_kind, network_metered, network_downlink_kbps
	) VALUES (
		1700000005000, 'req-legacy', '1:0:1:ABCD', 'live', 'quality', 'quality', 'transcode',
		'fmp4', 'hevc', 'aac', 1920, 1080, 25, 'host-fp', ?, 'legacy-caps-hash', 'ethernet', 1, 950000
	);
	PRAGMA user_version = 1;
	`, identity.Fingerprint(), string(capsJSON), identity.Fingerprint())
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := NewSqliteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	var version int
	require.NoError(t, store.DB.QueryRow(`PRAGMA user_version`).Scan(&version))
	assert.Equal(t, 8, version)

	var columnCount int
	require.NoError(t, store.DB.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('capability_observations')
		WHERE name IN ('source_fingerprint', 'observation_kind', 'outcome', 'session_id', 'feedback_event', 'feedback_code', 'feedback_message')
	`).Scan(&columnCount))
	assert.Equal(t, 7, columnCount)

	var sourceTableCount int
	require.NoError(t, store.DB.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name = 'capability_sources'
	`).Scan(&sourceTableCount))
	assert.Equal(t, 1, sourceTableCount)

	var sourceColumnCount int
	require.NoError(t, store.DB.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('capability_sources')
		WHERE name IN ('receiver_context_json', 'bitrate_confidence', 'bitrate_bucket', 'signal_fps')
	`).Scan(&sourceColumnCount))
	assert.Equal(t, 4, sourceColumnCount)

	var deviceColumnCount int
	require.NoError(t, store.DB.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('capability_devices')
		WHERE name IN ('brand', 'product', 'device_name')
	`).Scan(&deviceColumnCount))
	assert.Equal(t, 3, deviceColumnCount)

	gotCaps, ok, err := store.LookupCapabilities(context.Background(), identity)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"aac", "ac3"}, gotCaps.AudioCodecs)
	assert.Equal(t, []string{"h264", "hevc"}, gotCaps.VideoCodecs)
	assert.Equal(t, []string{"hls", "mp4"}, gotCaps.Containers)

	observation, ok, err := store.LookupDecisionObservation(context.Background(), "req-legacy")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "decision", observation.ObservationKind)
	assert.Equal(t, "predicted", observation.Outcome)
	assert.Equal(t, "", observation.SessionID)
	assert.Equal(t, "", observation.SourceFingerprint)
	require.NotNil(t, observation.Network)
	assert.Equal(t, "ethernet", observation.Network.Kind)
	require.NotNil(t, observation.Network.Metered)
	assert.True(t, *observation.Network.Metered)
	assert.Equal(t, 950000, observation.Network.DownlinkKbps)

	sourceSnapshot := SourceSnapshot{
		SubjectKind:       "live",
		Origin:            "live_scan",
		Container:         "ts",
		VideoCodec:        "h264",
		AudioCodec:        "aac",
		BitrateConfidence: "low",
		BitrateBucket:     "sub5m",
		Width:             1280,
		Height:            720,
		FPS:               50,
		SignalFPS:         50,
		UpdatedAt:         time.Unix(1_700_000_300, 0).UTC(),
	}
	require.NoError(t, store.RememberSource(context.Background(), sourceSnapshot))
	require.NoError(t, store.RecordObservation(context.Background(), PlaybackObservation{
		ObservedAt:         time.Unix(1_700_000_400, 0).UTC(),
		RequestID:          "req-legacy",
		ObservationKind:    "decision",
		Outcome:            "confirmed",
		SessionID:          "sess-legacy",
		SourceRef:          "1:0:1:ABCD",
		SourceFingerprint:  sourceSnapshot.Fingerprint(),
		SubjectKind:        "live",
		RequestedIntent:    "quality",
		ResolvedIntent:     "quality",
		Mode:               "transcode",
		SelectedContainer:  "fmp4",
		SelectedVideoCodec: "h264",
		SelectedAudioCodec: "aac",
		SourceWidth:        1280,
		SourceHeight:       720,
		SourceFPS:          50,
		HostFingerprint:    "host-fp",
		DeviceFingerprint:  identity.Fingerprint(),
		ClientCapsHash:     "legacy-caps-hash",
	}))

	rewrittenObservation, ok, err := store.LookupDecisionObservation(context.Background(), "req-legacy")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "confirmed", rewrittenObservation.Outcome)
	assert.Equal(t, "sess-legacy", rewrittenObservation.SessionID)
	assert.Equal(t, sourceSnapshot.Fingerprint(), rewrittenObservation.SourceFingerprint)
}
