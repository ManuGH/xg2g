// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name           string
		data           interface{}
		wantStatusCode int
	}{
		{
			name:           "simple map",
			data:           map[string]string{"status": "ok"},
			wantStatusCode: http.StatusOK,
		},
		{
			name:           "complex struct",
			data:           struct{ Name string }{Name: "test"},
			wantStatusCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.wantStatusCode, tt.data)

			assert.Equal(t, tt.wantStatusCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			// Verify valid JSON
			var result map[string]interface{}
			err := json.NewDecoder(w.Body).Decode(&result)
			require.NoError(t, err)
		})
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, assert.AnError)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result["error"])
}

func TestWriteUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	writeUnauthorized(w)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "unauthorized", result["error"])
}

func TestWriteForbidden(t *testing.T) {
	w := httptest.NewRecorder()
	writeForbidden(w)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "forbidden", result["error"])
}

func TestWriteNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	writeNotFound(w)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "not found", result["error"])
}

func TestWriteServiceUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	writeServiceUnavailable(w, assert.AnError)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)

	assert.NotEmpty(t, result["error"])
}

func TestSetDownloadHeaders(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantType string
	}{
		{
			name:     "xml file",
			filename: "guide.xml",
			wantType: "application/xml",
		},
		{
			name:     "m3u file",
			filename: "playlist.m3u",
			wantType: "audio/x-mpegurl",
		},
		{
			name:     "m3u8 file",
			filename: "playlist.m3u8",
			wantType: "audio/x-mpegurl",
		},
		{
			name:     "other file",
			filename: "data.bin",
			wantType: "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			now := time.Now()
			size := int64(1024)

			setDownloadHeaders(w, tt.filename, size, now)

			assert.Equal(t, tt.wantType, w.Header().Get("Content-Type"))
			assert.Equal(t, "1024", w.Header().Get("Content-Length"))
			assert.NotEmpty(t, w.Header().Get("Last-Modified"))
		})
	}
}
