// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package vod

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod/fsm"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestManager_DualWriteToSQLiteStore(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	store := sqlite.NewArtifactStore(db)
	require.NoError(t, store.InitSchema(ctx))

	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)

	mgr.SetArtifactStore(store)

	metaID := "rec-test-100:h264"
	mgr.SeedMetadata(metaID, Metadata{
		State:        ArtifactStatePreparing,
		ResolvedPath: "/tmp/rec-test-100.ts",
		UpdatedAt:    time.Now().UnixNano(),
	})

	// Verify dual-write created entry in SQLite ArtifactStore
	fetched, err := store.GetArtifact(ctx, "rec-test-100", "h264")
	require.NoError(t, err)
	require.Equal(t, "rec-test-100", fetched.RecordingRef)
	require.Equal(t, "h264", fetched.VariantHash)
	require.Equal(t, fsm.StatePreparing, fetched.State)

	// Transition state to READY via MarkProbed
	mgr.MarkProbed(metaID, "/tmp/rec-test-100.ts", &StreamInfo{
		Container: "mpegts",
		Video:     VideoStreamInfo{CodecName: "h264", Width: 1920, Height: 1080},
	}, nil)

	// Verify SQLite ArtifactStore state is updated to READY
	updated, err := store.GetArtifact(ctx, "rec-test-100", "h264")
	require.NoError(t, err)
	require.Equal(t, fsm.StateReady, updated.State)
}
