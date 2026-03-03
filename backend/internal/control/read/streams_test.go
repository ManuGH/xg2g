package read

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockStore for provider tests
type MockStore struct {
	Sessions []*model.SessionRecord
}

func (m *MockStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	return m.Sessions, nil
}

func TestGetStreams_Provider_StateStrictness(t *testing.T) {
	// Test 17: StateMapping_NoIdleEver
	// Also covers Test 15: TerminalStates_Filtered implicitly if we mix them

	sessions := []*model.SessionRecord{
		{SessionID: "new", State: model.SessionNew},         // Was "idle", now "active"
		{SessionID: "ready", State: model.SessionReady},     // "active"
		{SessionID: "active", State: model.SessionPriming},  // "active"
		{SessionID: "stopped", State: model.SessionStopped}, // Terminal -> Filtered
		{SessionID: "failed", State: model.SessionFailed},   // Terminal -> Filtered
	}

	store := &MockStore{Sessions: sessions}
	cfg := config.AppConfig{DataDir: t.TempDir()}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "missing.m3u"}} // No playlist needed for this test

	streams, err := GetStreams(context.Background(), cfg, snap, store, StreamsQuery{})
	require.NoError(t, err)

	// Check filtering
	require.Len(t, streams, 3, "Must filter out terminal states")

	// Check Strict State Mapping
	for _, s := range streams {
		assert.Equal(t, "active", s.State, "Session %s must be active", s.ID)
	}

	// Double check IDs present
	ids := make(map[string]bool)
	for _, s := range streams {
		ids[s.ID] = true
	}
	assert.True(t, ids["new"])
	assert.True(t, ids["ready"])
	assert.True(t, ids["active"])
	assert.False(t, ids["stopped"])
}

func TestGetStreams_Provider_InvalidPlaylistPath(t *testing.T) {
	store := &MockStore{Sessions: nil}
	cfg := config.AppConfig{DataDir: t.TempDir()}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "../escape.m3u"}}

	_, err := GetStreams(context.Background(), cfg, snap, store, StreamsQuery{})
	require.Error(t, err)
}

func TestGetStreams_Provider_NameResolution(t *testing.T) {
	// Test 23: NameResolution_UsesServiceRefNotTvgIdOnly

	// Setup Temp Playlist
	// ServiceRef: "1:0:1:TEST:REF" (from URL)
	// TvgID: "DIFFERENT_ID"
	// Name: "Correct Name"
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-id="DIFFERENT_ID",Correct Name
http://host/stream/1:0:1:TEST:REF:
`
	// Note: Trailing colon in URL should be trimmed by helper to "1:0:1:TEST:REF"

	tmpDir := t.TempDir()
	playlistPath := filepath.Join(tmpDir, "test.m3u")
	err := os.WriteFile(playlistPath, []byte(m3uContent), 0600)
	require.NoError(t, err)

	cfg := config.AppConfig{DataDir: tmpDir}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "test.m3u"}}

	// Session has ServiceRef matching the URL-derived ref, NOT the TvgID
	sessions := []*model.SessionRecord{
		{SessionID: "s1", State: model.SessionReady, ServiceRef: "1:0:1:TEST:REF"},
	}
	store := &MockStore{Sessions: sessions}

	streams, err := GetStreams(context.Background(), cfg, snap, store, StreamsQuery{})
	require.NoError(t, err)

	require.Len(t, streams, 1)
	assert.Equal(t, "Correct Name", streams[0].ChannelName, "Should resolve name via ServiceRef (URL derived)")
}

func TestGetStreams_Provider_NameResolution_CanonicalServiceRef(t *testing.T) {
	m3uContent := `#EXTM3U
#EXTINF:-1 tvg-id="DIFFERENT_ID",Correct Name
http://host/stream/1:0:1:TEST:REF:
`

	tmpDir := t.TempDir()
	playlistPath := filepath.Join(tmpDir, "test.m3u")
	err := os.WriteFile(playlistPath, []byte(m3uContent), 0600)
	require.NoError(t, err)

	cfg := config.AppConfig{DataDir: tmpDir}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "test.m3u"}}

	sessions := []*model.SessionRecord{
		{SessionID: "s1", State: model.SessionReady, ServiceRef: " 1:0:1:TEST:REF::  "},
	}
	store := &MockStore{Sessions: sessions}

	streams, err := GetStreams(context.Background(), cfg, snap, store, StreamsQuery{})
	require.NoError(t, err)
	require.Len(t, streams, 1)
	assert.Equal(t, "Correct Name", streams[0].ChannelName)
	assert.Equal(t, "1:0:1:TEST:REF", streams[0].ServiceRef)
}

func TestGetStreams_Provider_PlaylistOptimization_Test29(t *testing.T) {
	// Test 29: NameResolution_ParsesPlaylistOnce

	oldReader := PlaylistFileReader
	defer func() { PlaylistFileReader = oldReader }()

	calls := 0
	PlaylistFileReader = func(path string) ([]byte, error) {
		calls++
		return []byte(`#EXTM3U`), nil
	}

	cfg := config.AppConfig{DataDir: "."}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "mock.m3u"}}

	// Two records, should validly parse once even if multiple records exist
	sessions := []*model.SessionRecord{
		{SessionID: "s1", State: model.SessionReady},
		{SessionID: "s2", State: model.SessionReady},
	}
	store := &MockStore{Sessions: sessions}

	_, _ = GetStreams(context.Background(), cfg, snap, store, StreamsQuery{})

	assert.Equal(t, 1, calls, "Should read playlist exactly once per request")
}

func TestGetStreams_Provider_ThinClientAudit_Test28(t *testing.T) {
	// Test 28: ThinClientContract_ShapeAndSortingStable
	// Ensures no Sonderlogik needed for UI

	// Setup: Mixed input (some terminal, some new, some ready, mixed times)
	t1 := time.Now().Add(-10 * time.Minute)
	t2 := time.Now().Add(-5 * time.Minute) // Newer

	sessions := []*model.SessionRecord{
		{SessionID: "old", State: model.SessionReady, CreatedAtUnix: t1.Unix()},
		{SessionID: "new", State: model.SessionNew, CreatedAtUnix: t2.Unix()},     // New -> Active
		{SessionID: "fail", State: model.SessionFailed, CreatedAtUnix: t2.Unix()}, // Terminal -> Gone
	}
	store := &MockStore{Sessions: sessions}
	cfg := config.AppConfig{DataDir: t.TempDir()}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: "missing.m3u"}}

	streams, err := GetStreams(context.Background(), cfg, snap, store, StreamsQuery{})
	require.NoError(t, err)

	require.Len(t, streams, 2, "Only active streams returned")

	// 1. Sorting (StartedAt DESC -> Newest First)
	assert.Equal(t, "new", streams[0].ID)
	assert.Equal(t, "old", streams[1].ID)

	// 2. State Stability (Active Only)
	assert.Equal(t, "active", streams[0].State)
	assert.Equal(t, "active", streams[1].State)

	// 3. Shape Stability (Non-nil slice assumed by logic, confirmed by contract test)
}
