package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	ilog "github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubLogSource struct {
	entries []ilog.LogEntry
}

func (s stubLogSource) GetRecentLogs() []ilog.LogEntry {
	return s.entries
}

func TestGetLogsHonorsLimitQuery(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.logSource = stubLogSource{
		entries: []ilog.LogEntry{
			{Timestamp: time.Unix(100, 0).UTC(), Level: "info", Message: "first"},
			{Timestamp: time.Unix(200, 0).UTC(), Level: "warn", Message: "second"},
			{Timestamp: time.Unix(300, 0).UTC(), Level: "error", Message: "third"},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v3/logs?limit=2", nil)
	w := httptest.NewRecorder()
	limit := 2

	srv.GetLogs(w, req, GetLogsParams{Limit: &limit})

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body []LogEntry
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body, 2)
	require.NotNil(t, body[0].Message)
	require.NotNil(t, body[1].Message)
	assert.Equal(t, "third", *body[0].Message)
	assert.Equal(t, "second", *body[1].Message)
}
