// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
			assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

			// Verify valid JSON
			var result map[string]interface{}
			err := json.NewDecoder(w.Body).Decode(&result)
			require.NoError(t, err)
		})
	}
}
