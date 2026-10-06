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

func TestGetLogs_Scrubbing_WithAndWithoutResolver(t *testing.T) {
	rawRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/SECRET-CANARY-1/live/token-xyz-987/channel_prime.ts:Canary Channel 1"
	dvbRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"

	// 1. Without resolver: raw IPTV reference is redacted, DVB reference is untouched
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.logSource = stubLogSource{
		entries: []ilog.LogEntry{
			{
				Timestamp: time.Now().UTC(),
				Level:     "info",
				Message:   "Tuning " + rawRef,
				Fields: map[string]any{
					"ref":     rawRef,
					"dvb":     dvbRef,
					"err_msg": "dial tcp: lookup canary.invalid: no such host",
				},
			},
		},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v3/logs", nil)
	srv.GetLogs(w, r, GetLogsParams{})
	require.Equal(t, http.StatusOK, w.Code)

	bodyStr := w.Body.String()
	assert.NotContains(t, bodyStr, "canary.invalid")
	assert.NotContains(t, bodyStr, "SECRET-CANARY-1")
	assert.NotContains(t, bodyStr, "token-xyz-987")
	assert.Contains(t, bodyStr, "[REDACTED_IPTV_REF]")
	assert.Contains(t, bodyStr, dvbRef)

	// 2. With resolver: raw IPTV reference is masked to opaque iptv_<id>
	res, canaryID, _ := setupCanaryResolver(t)
	srv.SetIPTVResolver(res)

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/api/v3/logs", nil)
	srv.GetLogs(w2, r2, GetLogsParams{})
	require.Equal(t, http.StatusOK, w2.Code)

	bodyStr2 := w2.Body.String()
	assert.NotContains(t, bodyStr2, "canary.invalid")
	assert.NotContains(t, bodyStr2, "SECRET-CANARY-1")
	assert.NotContains(t, bodyStr2, "token-xyz-987")
	assert.Contains(t, bodyStr2, string(canaryID))
	assert.Contains(t, bodyStr2, dvbRef)
}
