// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/exec/ffmpeg"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHLSStore struct {
	sessions map[string]*model.SessionRecord
}

func (m *mockHLSStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, os.ErrNotExist // Simulate "not found"
	}
	return s, nil
}

func TestServeHLS_HappyPath(t *testing.T) {
	// Setup
	tmpRoot := t.TempDir()
	sessID := "valid-sess"
	sessDir := ffmpeg.SessionOutputDir(tmpRoot, sessID)
	require.NoError(t, os.MkdirAll(sessDir, 0755))

	// Create index.m3u8
	err := os.WriteFile(filepath.Join(sessDir, "index.m3u8"), []byte("#EXTM3U"), 0644)
	require.NoError(t, err)

	store := &mockHLSStore{
		sessions: map[string]*model.SessionRecord{
			sessID: {State: model.SessionReady, ExpiresAtUnix: time.Now().Add(1 * time.Hour).Unix()},
		},
	}

	// Request
	req := httptest.NewRequest("GET", "/hls/index.m3u8", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpRoot, sessID, "index.m3u8")

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apple.mpegurl", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "#EXTM3U", string(body))
}

func TestServeHLS_NotReady(t *testing.T) {
	tmpRoot := t.TempDir()
	sessID := "starting-sess"

	store := &mockHLSStore{
		sessions: map[string]*model.SessionRecord{
			sessID: {State: model.SessionNew}, // Not READY
		},
	}

	req := httptest.NewRequest("GET", "/hls/index.m3u8", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, store, tmpRoot, sessID, "index.m3u8")

	assert.Equal(t, http.StatusNotFound, w.Result().StatusCode)
}

func TestServeHLS_PathTraversal(t *testing.T) {
	req := httptest.NewRequest("GET", "/hls/../secret", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, &mockHLSStore{}, "/tmp", "sess", "../secret")
	assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
}

func TestServeHLS_InvalidSessionID(t *testing.T) {
	req := httptest.NewRequest("GET", "/hls/index.m3u8", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, &mockHLSStore{}, "/tmp", "../bad", "index.m3u8")
	assert.Equal(t, http.StatusBadRequest, w.Result().StatusCode)
}

func TestServeHLS_ForbiddenExt(t *testing.T) {
	req := httptest.NewRequest("GET", "/hls/foo.exe", nil)
	w := httptest.NewRecorder()

	ServeHLS(w, req, &mockHLSStore{}, "/tmp", "sess", "foo.exe")
	assert.Equal(t, http.StatusForbidden, w.Result().StatusCode)
}
