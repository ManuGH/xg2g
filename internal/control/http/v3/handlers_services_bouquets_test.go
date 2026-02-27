package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetServicesBouquets_ResponseShapeAndCounts verifies that the endpoint returns
// a list of Bouquet objects with correct "name" and "services" counts, matching the OpenAPI spec.
func TestGetServicesBouquets_ResponseShapeAndCounts(t *testing.T) {
	// 1. Setup minimal environment with a playlist
	tmpDir := t.TempDir()
	playlistPath := filepath.Join(tmpDir, "buckets.m3u")

	// Create M3U with:
	// - Bouquet A: 2 entries
	// - Bouquet B: 1 entry
	// - Empty Group: 1 entry (should be ignored)
	// - Duplicate in A: Should be counted as distinct entry (total 2 for A)
	content := `#EXTM3U
#EXTINF:-1 group-title="Starts",Channel 1
rtmp://1
#EXTINF:-1 group-title="Starts",Channel 2
rtmp://2
#EXTINF:-1 group-title="Ends",Channel 3
rtmp://3
#EXTINF:-1,No Group
rtmp://4
`
	err := os.WriteFile(playlistPath, []byte(content), 0600)
	require.NoError(t, err)

	cfg := config.AppConfig{
		DataDir: tmpDir,
	}
	snap := config.Snapshot{
		Runtime: config.RuntimeSnapshot{
			PlaylistFilename: "buckets.m3u",
		},
	}

	server := &Server{
		cfg:  cfg,
		snap: snap,
	}

	// 2. Perform Request
	req := httptest.NewRequest("GET", "/api/v3/services/bouquets", nil)
	w := httptest.NewRecorder()

	server.GetServicesBouquets(w, req)

	// 3. Verify Response
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var list []Bouquet
	err = json.NewDecoder(resp.Body).Decode(&list)
	require.NoError(t, err)

	// Expecting [{"name":"Ends", "services":1}, {"name":"Starts", "services":2}]
	// "Starts" appears first in playlist, does "ordering by appearance" mean "Starts" first?
	// Our code accumulates in order of *first appearance* of the group.
	// "Starts" appears at line 2. "Ends" appears at line 6.
	// So "Starts" should be first.

	require.Len(t, list, 2)

	assert.Equal(t, "Starts", *list[0].Name)
	assert.Equal(t, 2, *list[0].Services)

	assert.Equal(t, "Ends", *list[1].Name)
	assert.Equal(t, 1, *list[1].Services)
}

// TestGetServicesBouquets_Fallback verifies fallback behavior when playlist matches NotExist
func TestGetServicesBouquets_Fallback(t *testing.T) {
	// 1. Setup with NO playlist file, but configured bouquets
	tmpDir := t.TempDir()

	cfg := config.AppConfig{
		DataDir: tmpDir,
		Bouquet: "Fallback1,Fallback2",
	}
	snap := config.Snapshot{
		Runtime: config.RuntimeSnapshot{
			PlaylistFilename: "missing.m3u",
		},
	}

	server := &Server{
		cfg:  cfg,
		snap: snap,
	}

	// 2. Perform Request
	req := httptest.NewRequest("GET", "/api/v3/services/bouquets", nil)
	w := httptest.NewRecorder()

	server.GetServicesBouquets(w, req)

	// 3. Verify Response
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var list []Bouquet
	err := json.NewDecoder(resp.Body).Decode(&list)
	require.NoError(t, err)

	require.Len(t, list, 2)
	assert.Equal(t, "Fallback1", *list[0].Name)
	assert.Equal(t, 0, *list[0].Services) // Truth: 0 known services
	assert.Equal(t, "Fallback2", *list[1].Name)
	assert.Equal(t, 0, *list[1].Services)
}

// TestGetServicesBouquets_EmptyFile verifies that an empty file returns valid empty array (status 200),
// NOT the fallback config.
func TestGetServicesBouquets_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.m3u")
	err := os.WriteFile(emptyPath, []byte(""), 0600)
	require.NoError(t, err)

	cfg := config.AppConfig{
		DataDir: tmpDir,
		Bouquet: "Fallback1", // Should be ignored
	}
	snap := config.Snapshot{
		Runtime: config.RuntimeSnapshot{
			PlaylistFilename: "empty.m3u",
		},
	}

	server := &Server{cfg: cfg, snap: snap}

	req := httptest.NewRequest("GET", "/api/v3/services/bouquets", nil)
	w := httptest.NewRecorder()

	server.GetServicesBouquets(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var list []Bouquet
	err = json.NewDecoder(resp.Body).Decode(&list)
	require.NoError(t, err)

	// Truth: File exists and is empty -> 0 bouquets. No fallback.
	assert.Len(t, list, 0)
}

// TestGetServicesBouquets_FailClosed verifies strict fail-closed on read error
func TestGetServicesBouquets_FailClosed(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a directory where a file is expected to trigger Read Error (EISDIR)
	// Running as root bypasses 0000 permissions, so directory is a safer error trigger.
	badPath := filepath.Join(tmpDir, "bad.m3u")
	err := os.Mkdir(badPath, 0750)
	require.NoError(t, err)

	cfg := config.AppConfig{
		DataDir: tmpDir,
		Bouquet: "Fallback1", // Should be IGNORED
	}
	snap := config.Snapshot{
		Runtime: config.RuntimeSnapshot{
			PlaylistFilename: "bad.m3u",
		},
	}

	server := &Server{cfg: cfg, snap: snap}

	req := httptest.NewRequest("GET", "/api/v3/services/bouquets", nil)
	w := httptest.NewRecorder()

	server.GetServicesBouquets(w, req)

	// Fail Closed -> 500
	resp := w.Result()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Verify it's a Problem JSON
	var prob map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&prob)
	require.NoError(t, err)
	assert.Equal(t, "/problems/services/read_failed", prob["type"])
}
