// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import "time"

// SeriesRule represents a rule for auto-recording series.
type SeriesRule struct {
	ID             string     `json:"id"`
	Enabled        bool       `json:"enabled"`
	Keyword        string     `json:"keyword"`
	ChannelRef     string     `json:"channel_ref,omitempty"`
	Days           []int      `json:"days,omitempty"`         // 0=Sunday
	StartWindow    string     `json:"start_window,omitempty"` // HHMM-HHMM
	Priority       int        `json:"priority"`
	LastRunAt      time.Time  `json:"last_run_at,omitempty"`
	LastRunStatus  string     `json:"last_run_status,omitempty"`
	LastRunSummary RunSummary `json:"last_run_summary,omitempty"`
}
