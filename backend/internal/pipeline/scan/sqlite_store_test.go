package scan

import (
	"path/filepath"
	"testing"
	"time"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSqliteStore_RoundTripsRetryMetadata(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capabilities.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	now := time.Now().UTC().Truncate(time.Second)
	store.Update(Capability{
		ServiceRef:         "1:0:1:FAILED",
		State:              CapabilityStateFailed,
		LastAttempt:        now,
		FailureReason:      "no_lock",
		NextRetryAt:        now.Add(24 * time.Hour),
		Container:          "ts",
		VideoCodec:         "h264",
		AudioCodec:         "aac",
		BitrateKbps:        9000,
		BitrateMeanKbps:    9600,
		BitratePeakKbps:    12000,
		BitrateSamples:     4,
		Width:              1280,
		Height:             720,
		FPS:                50,
		SignalFPS:          50,
		FieldOrder:         "tt",
		AudioChannels:      6,
		AudioBitrateKbps:   384,
		AudioSampleRate:    48000,
		AudioChannelLayout: "5.1(side)",
	})

	got, found := store.Get("1:0:1:FAILED")
	require.True(t, found)
	require.Equal(t, CapabilityStateFailed, got.State)
	require.True(t, got.LastAttempt.Equal(now))
	require.Equal(t, "no_lock", got.FailureReason)
	require.True(t, got.NextRetryAt.Equal(now.Add(24*time.Hour)))
	require.Equal(t, "ts", got.Container)
	require.Equal(t, "h264", got.VideoCodec)
	require.Equal(t, "aac", got.AudioCodec)
	require.Equal(t, 9000, got.BitrateKbps)
	require.Equal(t, 9600, got.BitrateMeanKbps)
	require.Equal(t, 12000, got.BitratePeakKbps)
	require.Equal(t, 4, got.BitrateSamples)
	require.Equal(t, 1280, got.Width)
	require.Equal(t, 720, got.Height)
	require.Equal(t, 50.0, got.FPS)
	require.Equal(t, 50.0, got.SignalFPS)
	require.Equal(t, "tt", got.FieldOrder)
	require.Equal(t, 6, got.AudioChannels)
	require.Equal(t, 384, got.AudioBitrateKbps)
	require.Equal(t, 48000, got.AudioSampleRate)
	require.Equal(t, "5.1(side)", got.AudioChannelLayout)
}

func TestSqliteStore_NormalizesLegacyIncompleteRows(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capabilities.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	legacyScan := time.Date(2026, 2, 10, 17, 59, 51, 0, time.UTC)
	_, err = store.DB.Exec(
		`INSERT INTO capabilities(service_ref, interlaced, last_scan, resolution, codec) VALUES (?, ?, ?, ?, ?)`,
		"1:0:19:102:B:85:C00000:0:0:0:", false, legacyScan.Format(time.RFC3339), "", "",
	)
	require.NoError(t, err)

	got, found := store.Get("1:0:19:102:B:85:C00000:0:0:0:")
	require.True(t, found)
	require.Equal(t, CapabilityStateFailed, got.State)
	require.True(t, got.RetryDue(legacyScan.Add(25*time.Hour)))
}

func TestSqliteStore_GetMatchesLegacyTrailingColonKey(t *testing.T) {
	store, err := NewSqliteStore(filepath.Join(t.TempDir(), "capabilities.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	now := time.Date(2026, 3, 23, 10, 13, 18, 0, time.UTC)
	_, err = store.DB.Exec(
		`INSERT INTO capabilities(service_ref, interlaced, last_scan, resolution, codec, scan_state, last_attempt, last_success, next_retry_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"1:0:19:132F:3EF:1:C00000:0:0:0:", false, now.Format(time.RFC3339), "1280x720", "h264", "ok", now.Format(time.RFC3339), now.Format(time.RFC3339), now.Add(24*time.Hour).Format(time.RFC3339),
	)
	require.NoError(t, err)

	got, found := store.Get("1:0:19:132F:3EF:1:C00000:0:0:0")
	require.True(t, found)
	require.Equal(t, "1:0:19:132F:3EF:1:C00000:0:0:0", got.ServiceRef)
	require.Equal(t, "1280x720", got.Resolution)
	require.Equal(t, "h264", got.Codec)
	require.False(t, got.Interlaced)
}

func TestSqliteStore_MigratesLegacySchemaDespiteMatchingUserVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "capabilities.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	require.NoError(t, err)

	legacyScan := time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC)
	_, err = db.Exec(`
	CREATE TABLE capabilities (
		service_ref TEXT PRIMARY KEY,
		interlaced BOOLEAN NOT NULL DEFAULT 0,
		last_scan TEXT NOT NULL,
		resolution TEXT NOT NULL,
		codec TEXT NOT NULL,
		fps REAL NOT NULL DEFAULT 0,
		bitrate_k INTEGER NOT NULL DEFAULT 0
	);
	INSERT INTO capabilities(service_ref, interlaced, last_scan, resolution, codec, fps, bitrate_k)
	VALUES ('1:0:19:132F:3EF:1:C00000:0:0:0:', 1, ?, '1920x1080', 'h264', 25, 9000);
	PRAGMA user_version = 3;
	`, legacyScan.Format(time.RFC3339))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := NewSqliteStore(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	var count int
	err = store.DB.QueryRow(`
	SELECT COUNT(*) FROM pragma_table_info('capabilities')
	WHERE name IN ('last_attempt', 'last_success', 'scan_state', 'failure_reason', 'next_retry_at', 'container', 'video_codec', 'audio_codec', 'bitrate_k', 'bitrate_mean_k', 'bitrate_peak_k', 'bitrate_samples', 'width', 'height', 'fps', 'signal_fps', 'field_order', 'audio_channels', 'audio_bitrate_k', 'audio_sample_rate', 'audio_channel_layout')
			`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 21, count)

	got, found := store.Get("1:0:19:132F:3EF:1:C00000:0:0:0")
	require.True(t, found)
	require.Equal(t, "1:0:19:132F:3EF:1:C00000:0:0:0", got.ServiceRef)
	require.Equal(t, "1920x1080", got.Resolution)
	require.Equal(t, "h264", got.Codec)
	require.Equal(t, "h264", got.VideoCodec)
	require.Equal(t, 9000, got.BitrateKbps)
	require.Equal(t, 9000, got.BitrateMeanKbps)
	require.Equal(t, 9000, got.BitratePeakKbps)
	require.Equal(t, 1, got.BitrateSamples)
	require.Equal(t, CapabilityStatePartial, got.State)
	require.True(t, got.Interlaced)
	require.True(t, got.LastAttempt.Equal(legacyScan))
	require.True(t, got.LastSuccess.Equal(legacyScan))
	require.True(t, got.NextRetryAt.Equal(legacyScan.Add(partialRetryWindow)))

	store.Update(Capability{
		ServiceRef:         got.ServiceRef,
		State:              CapabilityStatePartial,
		LastScan:           legacyScan.Add(2 * time.Hour),
		LastAttempt:        legacyScan.Add(2 * time.Hour),
		LastSuccess:        legacyScan,
		FailureReason:      "legacy_migrated",
		NextRetryAt:        legacyScan.Add(8 * time.Hour),
		Container:          "mpegts",
		VideoCodec:         "hevc",
		AudioCodec:         "aac",
		BitrateKbps:        4800,
		BitrateMeanKbps:    5200,
		BitratePeakKbps:    6100,
		BitrateSamples:     3,
		Width:              1280,
		Height:             720,
		FPS:                50,
		SignalFPS:          50,
		FieldOrder:         "tt",
		AudioChannels:      2,
		AudioBitrateKbps:   192,
		AudioSampleRate:    48000,
		AudioChannelLayout: "stereo",
	})
	require.NoError(t, store.Close())

	store, err = NewSqliteStore(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	rewritten, found := store.Get("1:0:19:132F:3EF:1:C00000:0:0:0")
	require.True(t, found)
	require.Equal(t, CapabilityStatePartial, rewritten.State)
	require.Equal(t, "ts", rewritten.Container)
	require.Equal(t, "hevc", rewritten.VideoCodec)
	require.Equal(t, "aac", rewritten.AudioCodec)
	require.Equal(t, 4800, rewritten.BitrateKbps)
	require.Equal(t, 5200, rewritten.BitrateMeanKbps)
	require.Equal(t, 6100, rewritten.BitratePeakKbps)
	require.Equal(t, 3, rewritten.BitrateSamples)
	require.Equal(t, 1280, rewritten.Width)
	require.Equal(t, 720, rewritten.Height)
	require.Equal(t, 50.0, rewritten.FPS)
	require.Equal(t, 50.0, rewritten.SignalFPS)
	require.Equal(t, "tt", rewritten.FieldOrder)
	require.Equal(t, 2, rewritten.AudioChannels)
	require.Equal(t, 192, rewritten.AudioBitrateKbps)
	require.Equal(t, 48000, rewritten.AudioSampleRate)
	require.Equal(t, "stereo", rewritten.AudioChannelLayout)
	require.Equal(t, "legacy_migrated", rewritten.FailureReason)
	require.True(t, rewritten.NextRetryAt.Equal(legacyScan.Add(8*time.Hour)))
}

func TestCapability_RetryDueTreatsIncompleteMediaTruthAsPartial(t *testing.T) {
	lastAttempt := time.Date(2026, 3, 23, 10, 13, 18, 0, time.UTC)
	cap := Capability{
		ServiceRef:  "1:0:19:132F:3EF:1:C00000:0:0:0:",
		State:       CapabilityStateOK,
		LastScan:    lastAttempt,
		LastAttempt: lastAttempt,
		LastSuccess: lastAttempt,
		NextRetryAt: lastAttempt.Add(30 * 24 * time.Hour),
		Resolution:  "1280x720",
		Codec:       "h264",
	}

	normalized := cap.Normalized()
	require.Equal(t, CapabilityStatePartial, normalized.State)
	require.False(t, cap.RetryDue(lastAttempt.Add(partialRetryWindow-time.Second)))
	require.True(t, cap.RetryDue(lastAttempt.Add(partialRetryWindow)))
}

func TestCapability_StableBitrateKbps_PrefersConservativeStableEstimate(t *testing.T) {
	cap := Capability{
		BitrateKbps:     7200,
		BitrateMeanKbps: 8400,
		BitratePeakKbps: 12000,
		BitrateSamples:  5,
	}

	require.Equal(t, 10200, cap.StableBitrateKbps())
	require.Equal(t, "high", cap.BitrateConfidence())
}

func TestCapability_WithObservedBitrateKbps_AccumulatesRollingStats(t *testing.T) {
	cap := Capability{}
	cap = cap.WithObservedBitrateKbps(8000)
	cap = cap.WithObservedBitrateKbps(12000)

	require.Equal(t, 12000, cap.BitrateKbps)
	require.Equal(t, 10000, cap.BitrateMeanKbps)
	require.Equal(t, 12000, cap.BitratePeakKbps)
	require.Equal(t, 2, cap.BitrateSamples)
	require.Equal(t, "medium", cap.BitrateConfidence())
}
