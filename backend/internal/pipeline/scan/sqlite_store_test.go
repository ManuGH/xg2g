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
		ServiceRef:    "1:0:1:FAILED",
		State:         CapabilityStateFailed,
		LastAttempt:   now,
		FailureReason: "no_lock",
		NextRetryAt:   now.Add(24 * time.Hour),
		Container:     "ts",
		VideoCodec:    "h264",
		AudioCodec:    "aac",
		Width:         1280,
		Height:        720,
		FPS:           50,
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
	require.Equal(t, 1280, got.Width)
	require.Equal(t, 720, got.Height)
	require.Equal(t, 50.0, got.FPS)
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
	PRAGMA user_version = 3;
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := NewSqliteStore(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	var count int
	err = store.DB.QueryRow(`
	SELECT COUNT(*) FROM pragma_table_info('capabilities')
	WHERE name IN ('last_attempt', 'last_success', 'scan_state', 'failure_reason', 'next_retry_at', 'container', 'video_codec', 'audio_codec', 'width', 'height', 'fps')
	`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 11, count)
}
