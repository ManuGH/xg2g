package read

import (
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// LogEntry captures a single log event for read-only consumption.
type LogEntry struct {
	Level   string
	Message string
	Time    time.Time
	Fields  map[string]interface{}
}

// LogSource defines the minimal interface required to fetch log data.
type LogSource interface {
	GetRecentLogs() []log.LogEntry
}

// GetRecentLogs returns the most recent log entries formatted for the read model.
func GetRecentLogs(src LogSource) ([]LogEntry, error) {
	if src == nil {
		return nil, nil
	}
	internalLogs := src.GetRecentLogs()

	entries := make([]LogEntry, len(internalLogs))
	for i, l := range internalLogs {
		entries[i] = LogEntry{
			Level:   l.Level,
			Message: l.Message,
			Time:    l.Timestamp,
			Fields:  l.Fields,
		}
	}

	return entries, nil
}
