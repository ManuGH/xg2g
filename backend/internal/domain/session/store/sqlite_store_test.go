package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestSqliteStore_Pragmas(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_pragmas.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 1. Check Journal Mode
	var mode string
	err = store.DB.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil || mode != "wal" {
		t.Errorf("expected WAL mode, got %s (err: %v)", mode, err)
	}

	// 2. Check Synchronous
	var sync int
	err = store.DB.QueryRow("PRAGMA synchronous").Scan(&sync)
	if err != nil || sync != 1 { // 1 = NORMAL
		t.Errorf("expected synchronous=NORMAL (1), got %d", sync)
	}

	// 3. Check Busy Timeout
	var timeout int
	err = store.DB.QueryRow("PRAGMA busy_timeout").Scan(&timeout)
	if err != nil || timeout != 5000 {
		t.Errorf("expected busy_timeout=5000, got %d", timeout)
	}

	// 4. Check Foreign Keys
	var fk int
	err = store.DB.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if err != nil || fk != 1 {
		t.Errorf("expected foreign_keys=ON (1), got %d", fk)
	}
}

func TestSqliteStore_CrashSafeReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_crash.db")

	// Write data
	s1, _ := NewSqliteStore(dbPath)
	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID:     "sess-crash",
		State:         model.SessionNew,
		CreatedAtUnix: time.Now().Unix(),
	}
	if err := s1.PutSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	// Reopen and Verify
	s2, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got, err := s2.GetSession(ctx, "sess-crash")
	if err != nil || got == nil || got.SessionID != "sess-crash" {
		t.Errorf("recovery failed: %v", err)
	}
}

func TestSqliteStore_PlaybackTraceRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_trace.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID: "sess-trace",
		State:     model.SessionReady,
		PlaybackTrace: &model.PlaybackTrace{
			RequestProfile:    "compatible",
			ClientPath:        "hlsjs",
			InputKind:         "receiver",
			TargetProfileHash: "hash-trace",
			HLS: &model.HLSAccessTrace{
				PlaylistRequestCount:   3,
				LastPlaylistAtUnix:     101,
				LastPlaylistIntervalMs: 2100,
				SegmentRequestCount:    2,
				LastSegmentAtUnix:      102,
				LastSegmentName:        "seg_000042.ts",
				LastSegmentGapMs:       1900,
				LatestSegmentLagMs:     1200,
				StallRisk:              "low",
				StartupMode:            "trace_guarded",
				StartupHeadroomSec:     10,
				StartupReasons:         []string{"client_family_native", "segment_cadence_guard"},
			},
			TargetProfile: &playbackprofile.TargetPlaybackProfile{
				Container: "mpegts",
				Packaging: playbackprofile.PackagingTS,
				Video: playbackprofile.VideoTarget{
					Mode:  playbackprofile.MediaModeCopy,
					Codec: "h264",
				},
				Audio: playbackprofile.AudioTarget{
					Mode:        playbackprofile.MediaModeTranscode,
					Codec:       "aac",
					Channels:    2,
					BitrateKbps: 256,
				},
			},
			Fallbacks: []model.PlaybackFallbackTrace{{
				AtUnix:          42,
				Trigger:         "mediaError",
				Reason:          "bufferAppendError",
				FromProfileHash: "hash-old",
				ToProfileHash:   "hash-new",
			}},
			StopClass:  model.PlaybackStopClassPackager,
			StopReason: "playlist_not_ready",
		},
	}

	if err := store.PutSession(ctx, rec); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetSession(ctx, "sess-trace")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PlaybackTrace == nil {
		t.Fatalf("expected playback trace roundtrip, got %#v", got)
	}
	if got.PlaybackTrace.TargetProfileHash != "hash-trace" {
		t.Fatalf("unexpected target profile hash: %q", got.PlaybackTrace.TargetProfileHash)
	}
	if len(got.PlaybackTrace.Fallbacks) != 1 || got.PlaybackTrace.Fallbacks[0].Trigger != "mediaError" {
		t.Fatalf("unexpected fallback trace: %#v", got.PlaybackTrace.Fallbacks)
	}
	if got.PlaybackTrace.StopClass != model.PlaybackStopClassPackager {
		t.Fatalf("unexpected stop class: %q", got.PlaybackTrace.StopClass)
	}
	if got.PlaybackTrace.HLS == nil {
		t.Fatalf("expected hls trace roundtrip, got %#v", got.PlaybackTrace)
	}
	if got.PlaybackTrace.HLS.LastSegmentName != "seg_000042.ts" ||
		got.PlaybackTrace.HLS.StallRisk != "low" ||
		got.PlaybackTrace.HLS.StartupMode != "trace_guarded" ||
		got.PlaybackTrace.HLS.StartupHeadroomSec != 10 {
		t.Fatalf("unexpected hls trace: %#v", got.PlaybackTrace.HLS)
	}
	if len(got.PlaybackTrace.HLS.StartupReasons) != 2 ||
		got.PlaybackTrace.HLS.StartupReasons[0] != "client_family_native" ||
		got.PlaybackTrace.HLS.StartupReasons[1] != "segment_cadence_guard" {
		t.Fatalf("unexpected hls trace: %#v", got.PlaybackTrace.HLS)
	}
}

func TestSqliteStore_ReasonDetailRoundTrip_UsesExplicitColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_reason_detail.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID:         "sess-reason-detail",
		State:             model.SessionFailed,
		Reason:            model.RUpstreamCorrupt,
		ReasonDetailCode:  model.DInvalidUpstreamInput,
		ReasonDetailDebug: "invalid PAT/PMT sequence",
		CreatedAtUnix:     111,
		UpdatedAtUnix:     222,
	}
	if err := store.PutSession(ctx, rec); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetSession(ctx, rec.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected stored session")
	}
	if got.Reason != rec.Reason {
		t.Fatalf("reason mismatch: got %q want %q", got.Reason, rec.Reason)
	}
	if got.ReasonDetailCode != rec.ReasonDetailCode {
		t.Fatalf("reason detail code mismatch: got %q want %q", got.ReasonDetailCode, rec.ReasonDetailCode)
	}
	if got.ReasonDetailDebug != rec.ReasonDetailDebug {
		t.Fatalf("reason detail debug mismatch: got %q want %q", got.ReasonDetailDebug, rec.ReasonDetailDebug)
	}

	var legacyDetail sql.NullString
	var detailCode sql.NullString
	var detailDebug sql.NullString
	if err := store.DB.QueryRowContext(ctx, `
		SELECT reason_detail, reason_detail_code, reason_detail_debug
		FROM sessions
		WHERE session_id = ?
	`, rec.SessionID).Scan(&legacyDetail, &detailCode, &detailDebug); err != nil {
		t.Fatal(err)
	}
	if legacyDetail.Valid {
		t.Fatalf("expected legacy reason_detail column to stay NULL on new writes, got %q", legacyDetail.String)
	}
	if !detailCode.Valid || detailCode.String != string(rec.ReasonDetailCode) {
		t.Fatalf("unexpected reason_detail_code column: %#v", detailCode)
	}
	if !detailDebug.Valid || detailDebug.String != rec.ReasonDetailDebug {
		t.Fatalf("unexpected reason_detail_debug column: %#v", detailDebug)
	}
}

func TestSqliteStore_ReasonDetailLegacyFallbackAndRewrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_reason_legacy.db")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	legacyDebug := "legacy free-text detail"
	if _, err := store.DB.ExecContext(ctx, `
		INSERT INTO sessions (
			session_id, service_ref, profile_json, state, pipeline_state, reason,
			reason_detail, reason_detail_code, reason_detail_debug,
			fallback_reason, correlation_id, created_at_ms, updated_at_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval, stop_reason
		) VALUES (?, 'svc', '{}', 'FAILED', 'INIT', 'R_INTERNAL_INVARIANT_BREACH', ?, NULL, NULL, '', 'corr', 1000, 1000, 0, 0, 0, '')
	`, "sess-legacy-reason", legacyDebug); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetSession(ctx, "sess-legacy-reason")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected legacy session")
	}
	if got.ReasonDetailDebug != legacyDebug {
		t.Fatalf("legacy fallback mismatch: got %q want %q", got.ReasonDetailDebug, legacyDebug)
	}
	if got.ReasonDetailCode != "" {
		t.Fatalf("expected empty reason detail code for legacy row, got %q", got.ReasonDetailCode)
	}

	if _, err := store.UpdateSession(ctx, got.SessionID, func(rec *model.SessionRecord) error {
		rec.State = model.SessionStopped
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var legacyColumn sql.NullString
	var debugColumn sql.NullString
	if err := store.DB.QueryRowContext(ctx, `
		SELECT reason_detail, reason_detail_debug
		FROM sessions
		WHERE session_id = ?
	`, got.SessionID).Scan(&legacyColumn, &debugColumn); err != nil {
		t.Fatal(err)
	}
	if legacyColumn.Valid {
		t.Fatalf("expected legacy reason_detail column to be cleared on rewrite, got %q", legacyColumn.String)
	}
	if !debugColumn.Valid || debugColumn.String != legacyDebug {
		t.Fatalf("expected rewritten reason_detail_debug to preserve legacy detail, got %#v", debugColumn)
	}

	rewritten, err := store.GetSession(ctx, got.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if rewritten == nil || rewritten.ReasonDetailDebug != legacyDebug {
		t.Fatalf("rewritten session lost debug detail: %#v", rewritten)
	}
}

func TestSqliteStore_Concurrency_WAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_concurrency.db")
	writerStore, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writerStore.Close()
	readerStore, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer readerStore.Close()

	ctx := context.Background()
	if err := writerStore.PutSession(ctx, &model.SessionRecord{
		SessionID:     "wal-baseline",
		State:         model.SessionNew,
		CreatedAtUnix: 1,
		UpdatedAtUnix: 1,
	}); err != nil {
		t.Fatal(err)
	}

	writerReady := make(chan struct{})
	releaseWriter := make(chan struct{})
	writerErr := make(chan error, 1)
	go func() {
		tx, err := writerStore.DB.BeginTx(ctx, nil)
		if err != nil {
			writerErr <- err
			return
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sessions (
				session_id, service_ref, profile_json, state, pipeline_state, reason,
				fallback_reason, correlation_id, created_at_ms, updated_at_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval, stop_reason
			) VALUES (?, 'svc', '{}', 'NEW', 'INIT', 'R_NONE', '', 'id', 0, 0, 0, 0, 0, '')
		`, "concurrent-1"); err != nil {
			_ = tx.Rollback()
			writerErr <- err
			return
		}
		close(writerReady)
		<-releaseWriter
		writerErr <- tx.Commit()
	}()

	<-writerReady
	readDone := make(chan error, 1)
	go func() {
		_, err := readerStore.GetSession(ctx, "wal-baseline")
		readDone <- err
	}()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("reader failed while writer tx open: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reader blocked while writer transaction was still open")
	}

	close(releaseWriter)
	if err := <-writerErr; err != nil {
		t.Fatalf("writer failed: %v", err)
	}

	got, err := readerStore.GetSession(ctx, "concurrent-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected committed writer row to be visible after commit")
	}
}

func TestSqliteStore_Idempotency_monotonic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_idem.db")
	store, _ := NewSqliteStore(dbPath)
	defer store.Close()

	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID: "s1",
		State:     model.SessionNew,
	}

	// 1. Put
	sid1, exists1, err := store.PutSessionWithIdempotency(ctx, rec, "key1", time.Hour)
	if err != nil || exists1 || sid1 != "s1" {
		t.Errorf("first put failed: %v, %v", err, exists1)
	}

	// 2. Replay same key, different session ID (should return s1)
	rec2 := &model.SessionRecord{SessionID: "s2", State: model.SessionNew}
	sid2, exists2, err := store.PutSessionWithIdempotency(ctx, rec2, "key1", time.Hour)
	if err != nil || !exists2 || sid2 != "s1" {
		t.Errorf("replay failed: %v, %v, got %s", err, exists2, sid2)
	}
}

func TestSqliteStore_Lease_Contention(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_lease.db")
	store, _ := NewSqliteStore(dbPath)
	defer store.Close()

	ctx := context.Background()

	// 1. Acquire
	_, ok, _ := store.TryAcquireLease(ctx, "res", "owner1", 100*time.Millisecond)
	if !ok {
		t.Fatal("acquire failed")
	}

	// 2. Contention (Different owner, not expired)
	_, ok, _ = store.TryAcquireLease(ctx, "res", "owner2", 100*time.Millisecond)
	if ok {
		t.Error("expected contention fail for owner2")
	}

	// 3. Renew (Same owner)
	_, ok, _ = store.RenewLease(ctx, "res", "owner1", 200*time.Millisecond)
	if !ok {
		t.Error("renew failed for owner1")
	}

	// 4. Takeover (Takeover after expiry)
	time.Sleep(250 * time.Millisecond)
	_, ok, _ = store.TryAcquireLease(ctx, "res", "owner2", 100*time.Millisecond)
	if !ok {
		t.Error("takeover failed after expiry")
	}
}
