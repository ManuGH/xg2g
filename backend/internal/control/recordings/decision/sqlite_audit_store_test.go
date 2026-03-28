package decision

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSqliteAuditStore_SuppressesHistoryWhenOnlyBasisChanges(t *testing.T) {
	store, err := NewSqliteAuditStore(filepath.Join(t.TempDir(), "decision_audit.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.DB.Close()) }()

	baseTime := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	event := testDecisionEvent(baseTime, "basis-a", "output-a")
	require.NoError(t, store.Record(context.Background(), event))

	event.BasisHash = "basis-b"
	event.HostPressureBand = string(playbackprofile.HostPressureConstrained)
	event.DecidedAt = baseTime.Add(time.Minute)
	require.NoError(t, store.Record(context.Background(), event))
	event = event.Normalized()

	require.Equal(t, 1, countDecisionHistoryRows(t, store))

	var basisHash string
	var changedAtMS int64
	var lastSeenAtMS int64
	err = store.DB.QueryRow(
		`SELECT basis_hash, changed_at_ms, last_seen_at_ms
		FROM decision_current
		WHERE service_ref = ? AND subject_kind = ? AND origin = ? AND client_family = ? AND requested_intent = ?`,
		event.ServiceRef,
		event.SubjectKind,
		event.Origin,
		event.ClientFamily,
		event.RequestedIntent,
	).Scan(&basisHash, &changedAtMS, &lastSeenAtMS)
	require.NoError(t, err)
	require.Equal(t, "basis-b", basisHash)
	require.Equal(t, baseTime.UnixMilli(), changedAtMS)
	require.Equal(t, baseTime.Add(time.Minute).UnixMilli(), lastSeenAtMS)
}

func TestSqliteAuditStore_PrunesHistoryByTTL(t *testing.T) {
	store, err := NewSqliteAuditStore(filepath.Join(t.TempDir(), "decision_audit.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.DB.Close()) }()

	oldEvent := testDecisionEvent(time.Now().UTC().Add(-(historyRetention + 24*time.Hour)), "basis-old", "output-old")
	require.NoError(t, store.Record(context.Background(), oldEvent))

	newEvent := testDecisionEvent(time.Now().UTC(), "basis-new", "output-new")
	require.NoError(t, store.Record(context.Background(), newEvent))

	require.Equal(t, 1, countDecisionHistoryRows(t, store))

	var basisHash string
	err = store.DB.QueryRow(`SELECT basis_hash FROM decision_history LIMIT 1`).Scan(&basisHash)
	require.NoError(t, err)
	require.Equal(t, "basis-new", basisHash)
}

func TestSqliteAuditStore_PrunesHistoryByDecidedAtNotInsertionOrder(t *testing.T) {
	store, err := NewSqliteAuditStore(filepath.Join(t.TempDir(), "decision_audit.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.DB.Close()) }()

	baseTime := time.Date(2026, 3, 27, 11, 0, 0, 0, time.UTC)
	for i := 1; i <= historyEntriesPerKey; i++ {
		event := testDecisionEvent(baseTime.Add(time.Duration(i)*time.Minute), "basis-"+timeLabel(i), "output-"+timeLabel(i))
		event.Selected.Container = "container-" + timeLabel(i)
		event.OutputHash = "output-" + timeLabel(i)
		require.NoError(t, store.Record(context.Background(), event))
	}

	oldestLate := testDecisionEvent(baseTime, "basis-00", "output-00")
	oldestLate.Selected.Container = "container-00"
	oldestLate.OutputHash = "output-00"
	require.NoError(t, store.Record(context.Background(), oldestLate))
	oldestLate = oldestLate.Normalized()

	require.Equal(t, historyEntriesPerKey, countDecisionHistoryRows(t, store))

	var oldestBasisHash string
	err = store.DB.QueryRow(`SELECT basis_hash FROM decision_history ORDER BY decided_at_ms ASC, id ASC LIMIT 1`).Scan(&oldestBasisHash)
	require.NoError(t, err)
	require.Equal(t, "basis-01", oldestBasisHash)

	var currentBasisHash string
	err = store.DB.QueryRow(
		`SELECT basis_hash FROM decision_current
		WHERE service_ref = ? AND subject_kind = ? AND origin = ? AND client_family = ? AND requested_intent = ?`,
		oldestLate.ServiceRef,
		oldestLate.SubjectKind,
		oldestLate.Origin,
		oldestLate.ClientFamily,
		oldestLate.RequestedIntent,
	).Scan(&currentBasisHash)
	require.NoError(t, err)
	require.Equal(t, "basis-20", currentBasisHash)
}

func TestSqliteAuditStore_SeparatesRuntimeAndSweepCurrentRows(t *testing.T) {
	store, err := NewSqliteAuditStore(filepath.Join(t.TempDir(), "decision_audit.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.DB.Close()) }()

	runtimeEvent := testDecisionEvent(time.Now().UTC(), "basis-runtime", "output-runtime")
	runtimeEvent.Origin = OriginRuntime
	require.NoError(t, store.Record(context.Background(), runtimeEvent))
	runtimeEvent = runtimeEvent.Normalized()

	sweepEvent := testDecisionEvent(time.Now().UTC().Add(time.Minute), "basis-sweep", "output-sweep")
	sweepEvent.Origin = OriginSweep
	require.NoError(t, store.Record(context.Background(), sweepEvent))
	sweepEvent = sweepEvent.Normalized()

	var count int
	err = store.DB.QueryRow(`SELECT COUNT(*) FROM decision_current`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	var runtimeBasis string
	err = store.DB.QueryRow(
		`SELECT basis_hash FROM decision_current
		WHERE service_ref = ? AND subject_kind = ? AND origin = ? AND client_family = ? AND requested_intent = ?`,
		runtimeEvent.ServiceRef,
		runtimeEvent.SubjectKind,
		OriginRuntime,
		runtimeEvent.ClientFamily,
		runtimeEvent.RequestedIntent,
	).Scan(&runtimeBasis)
	require.NoError(t, err)
	require.Equal(t, "basis-runtime", runtimeBasis)

	var sweepBasis string
	err = store.DB.QueryRow(
		`SELECT basis_hash FROM decision_current
		WHERE service_ref = ? AND subject_kind = ? AND origin = ? AND client_family = ? AND requested_intent = ?`,
		sweepEvent.ServiceRef,
		sweepEvent.SubjectKind,
		OriginSweep,
		sweepEvent.ClientFamily,
		sweepEvent.RequestedIntent,
	).Scan(&sweepBasis)
	require.NoError(t, err)
	require.Equal(t, "basis-sweep", sweepBasis)
}

func TestSqliteAuditStore_MigratesV1RowsToRuntimeOrigin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "decision_audit.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	_, err = db.Exec(`
	PRAGMA user_version = 1;
	CREATE TABLE decision_current (
		service_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		client_family TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		basis_hash TEXT NOT NULL,
		truth_hash TEXT NOT NULL,
		output_hash TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		target_profile_json TEXT,
		reasons_json TEXT NOT NULL,
		resolved_intent TEXT,
		host_pressure_band TEXT,
		client_caps_source TEXT,
		device_type TEXT,
		changed_at_ms INTEGER NOT NULL,
		last_seen_at_ms INTEGER NOT NULL,
		PRIMARY KEY (service_ref, subject_kind, client_family, requested_intent)
	);
	CREATE TABLE decision_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		client_family TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		basis_hash TEXT NOT NULL,
		truth_hash TEXT NOT NULL,
		output_hash TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		target_profile_json TEXT,
		reasons_json TEXT NOT NULL,
		resolved_intent TEXT,
		host_pressure_band TEXT,
		client_caps_source TEXT,
		device_type TEXT,
		decided_at_ms INTEGER NOT NULL
	);
	INSERT INTO decision_current(
		service_ref, subject_kind, client_family, requested_intent, basis_hash, truth_hash, output_hash,
		mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
		resolved_intent, host_pressure_band, client_caps_source, device_type, changed_at_ms, last_seen_at_ms
	) VALUES (
		'1:0:1:2B66:3F3:1:C00000:0:0:0:', 'live', 'safari', 'quality', 'basis-v1', 'truth-v1', 'output-v1',
		'transcode', 'fmp4', 'hevc', 'aac', NULL, '[]', 'quality', 'normal', 'runtime_plus_family', 'tv', 1000, 1000
	);
	INSERT INTO decision_history(
		service_ref, subject_kind, client_family, requested_intent, basis_hash, truth_hash, output_hash,
		mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
		resolved_intent, host_pressure_band, client_caps_source, device_type, decided_at_ms
	) VALUES (
		'1:0:1:2B66:3F3:1:C00000:0:0:0:', 'live', 'safari', 'quality', 'basis-v1', 'truth-v1', 'output-v1',
		'transcode', 'fmp4', 'hevc', 'aac', NULL, '[]', 'quality', 'normal', 'runtime_plus_family', 'tv', 1000
	);
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := NewSqliteAuditStore(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.DB.Close()) }()

	var version int
	require.NoError(t, store.DB.QueryRow(`PRAGMA user_version`).Scan(&version))
	require.Equal(t, auditSchemaVersion, version)

	var runtimeCount int
	err = store.DB.QueryRow(`SELECT COUNT(*) FROM decision_current WHERE origin = ?`, OriginRuntime).Scan(&runtimeCount)
	require.NoError(t, err)
	require.Equal(t, 1, runtimeCount)

	var historyRuntimeCount int
	err = store.DB.QueryRow(`SELECT COUNT(*) FROM decision_history WHERE origin = ?`, OriginRuntime).Scan(&historyRuntimeCount)
	require.NoError(t, err)
	require.Equal(t, 1, historyRuntimeCount)
}

func testDecisionEvent(decidedAt time.Time, basisHash, outputHash string) Event {
	return Event{
		ServiceRef:       "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind:      "live",
		Origin:           OriginRuntime,
		ClientFamily:     "safari",
		ClientCapsSource: "runtime_plus_family",
		DeviceType:       "tv",
		RequestedIntent:  "quality",
		ResolvedIntent:   "quality",
		Mode:             ModeTranscode,
		Selected: SelectedFormats{
			Container:  "fmp4",
			VideoCodec: "hevc",
			AudioCodec: "aac",
		},
		Reasons:    []ReasonCode{ReasonVideoCodecNotSupported},
		BasisHash:  basisHash,
		TruthHash:  "truth-a",
		OutputHash: outputHash,
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "fmp4",
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeTranscode,
				Codec: "hevc",
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeTranscode,
				Codec: "aac",
			},
			HLS: playbackprofile.HLSTarget{
				Enabled:          true,
				SegmentContainer: "fmp4",
				SegmentSeconds:   6,
			},
		},
		HostPressureBand: string(playbackprofile.HostPressureNormal),
		DecidedAt:        decidedAt,
	}
}

func countDecisionHistoryRows(t *testing.T, store *SqliteAuditStore) int {
	t.Helper()
	var count int
	err := store.DB.QueryRow(`SELECT COUNT(*) FROM decision_history`).Scan(&count)
	require.NoError(t, err)
	return count
}

func timeLabel(i int) string {
	return fmt.Sprintf("%02d", i)
}
