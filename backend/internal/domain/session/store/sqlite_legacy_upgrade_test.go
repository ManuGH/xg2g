// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/require"
)

// legacyV3SessionsSchema is the sessions table as it existed at schema version 3,
// before reason_detail_code/reason_detail_debug (v4) and playback_trace_json (v5)
// were added. migrate() upgrades such a DB with ALTER TABLE ADD COLUMN, which
// appends those columns at the physical END of the table.
const legacyV3SessionsSchema = `
CREATE TABLE sessions (
	session_id TEXT PRIMARY KEY,
	service_ref TEXT NOT NULL,
	profile_json TEXT NOT NULL,
	state TEXT NOT NULL,
	pipeline_state TEXT NOT NULL,
	reason TEXT NOT NULL,
	reason_detail TEXT,
	fallback_reason TEXT,
	fallback_at_ms INTEGER,
	correlation_id TEXT NOT NULL,
	created_at_ms INTEGER NOT NULL,
	updated_at_ms INTEGER NOT NULL,
	last_access_ms INTEGER,
	expires_at_ms INTEGER NOT NULL,
	lease_expires_at_ms INTEGER NOT NULL,
	heartbeat_interval INTEGER NOT NULL,
	last_heartbeat_ms INTEGER,
	stop_reason TEXT,
	latest_segment_at TEXT,
	last_playlist_access_at TEXT,
	playlist_published_at TEXT,
	context_data_json TEXT
);`

// TestSqliteStore_ReadsAfterLegacyColumnUpgrade verifies session reads survive a
// legacy-database upgrade. migrate() adds the v4/v5 columns with ALTER TABLE,
// appending them at the physical end of the table. SELECT * returns columns in
// physical order, so the appended columns arrive out of scanSession's positional
// order and corrupt (or error) every read. Reading by an explicit column list is
// order-stable.
func TestSqliteStore_ReadsAfterLegacyColumnUpgrade(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")

	// 1. Lay down a v3-era table and mark the DB as schema version 3.
	raw, err := sqlite.Open(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)
	_, err = raw.Exec(legacyV3SessionsSchema)
	require.NoError(t, err)
	_, err = raw.Exec("PRAGMA user_version = 3")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	// 2. Open via the store: migrate() ALTERs the v4/v5 columns onto the end.
	st, err := NewSqliteStore(dbPath)
	require.NoError(t, err)
	defer func() { _ = st.Close() }()

	// 3. Round-trip a record. ReasonDetailCode lives in an appended column;
	// HeartbeatInterval is a non-nullable int that a misaligned SELECT * would
	// try to scan from a TEXT/NULL column and error on.
	ctx := context.Background()
	rec := &model.SessionRecord{
		SessionID:         "sess-legacy",
		ServiceRef:        "1:0:1:abc",
		State:             model.SessionReady,
		PipelineState:     "RUNNING",
		Reason:            "R_OK",
		ReasonDetailCode:  model.ReasonDetailCode("R_DETAIL_TEST"),
		CorrelationID:     "corr-xyz",
		HeartbeatInterval: 7,
	}
	require.NoError(t, st.PutSession(ctx, rec))

	got, err := st.GetSession(ctx, "sess-legacy")
	require.NoError(t, err) // SELECT * would misalign and error/corrupt here
	require.NotNil(t, got)
	require.Equal(t, "1:0:1:abc", got.ServiceRef)
	require.Equal(t, model.SessionReady, got.State)
	require.Equal(t, "corr-xyz", got.CorrelationID)
	require.Equal(t, 7, got.HeartbeatInterval)
	require.Equal(t, model.ReasonDetailCode("R_DETAIL_TEST"), got.ReasonDetailCode)
}
