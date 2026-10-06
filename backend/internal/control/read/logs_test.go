// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package read

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLogSource struct {
	entries []log.LogEntry
}

func (m mockLogSource) GetRecentLogs() []log.LogEntry {
	return m.entries
}

func TestGetRecentLogs_Mapping(t *testing.T) {
	now := time.Now().UTC()
	src := mockLogSource{
		entries: []log.LogEntry{
			{
				Timestamp: now,
				Level:     "info",
				Message:   "Test log message",
				Fields: map[string]any{
					"key": "value",
				},
			},
		},
	}

	entries, err := GetRecentLogs(src)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "info", entries[0].Level)
	assert.Equal(t, "Test log message", entries[0].Message)
	assert.Equal(t, now, entries[0].Time)
	assert.Equal(t, "value", entries[0].Fields["key"])

	// Nil source test
	nilEntries, err := GetRecentLogs(nil)
	require.NoError(t, err)
	assert.Nil(t, nilEntries)
}
