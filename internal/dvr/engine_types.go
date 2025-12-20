// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"time"
)

// SeriesRuleRunReport is the persisted report for a single execution of a series rule.
// It is designed to be bounded and explainable.
type SeriesRuleRunReport struct {
	RuleID     string       `json:"ruleId"`
	RunID      string       `json:"runId"`   // Unique ID for this specific run
	Trigger    string       `json:"trigger"` // "manual" | "scheduled"
	StartedAt  time.Time    `json:"startedAt"`
	FinishedAt time.Time    `json:"finishedAt"`
	DurationMs int64        `json:"durationMs"`
	WindowFrom int64        `json:"windowFrom"` // unix seconds
	WindowTo   int64        `json:"windowTo"`   // unix seconds
	Status     string       `json:"status"`     // "success" | "partial" | "failed"
	Summary    RunSummary   `json:"summary"`
	Snapshot   RuleSnapshot `json:"snapshot"`

	// Bounded logs (Ring buffer logic should be applied by the engine)
	Decisions []RunDecision `json:"decisions,omitempty"` // Cap at ~200
	Errors    []RunError    `json:"errors,omitempty"`    // Cap at ~50
	Conflicts []RunConflict `json:"conflicts,omitempty"` // Cap at ~50
}

// RuleSnapshot captures the state of the rule at the time of the run.
// This provides context for the decisions made without needing to look up the current rule state.
type RuleSnapshot struct {
	ID          string `json:"id"`
	Enabled     bool   `json:"enabled"`
	Keyword     string `json:"keyword"`
	ChannelRef  string `json:"channelRef,omitempty"`
	Days        []int  `json:"days,omitempty"`        // 0=Sunday
	StartWindow string `json:"startWindow,omitempty"` // HH:MM-HH:MM
	Priority    int    `json:"priority"`
}

// RunSummary provides high-level counters and guardrail flags.
type RunSummary struct {
	EpgItemsScanned  int `json:"epgItemsScanned"`
	EpgItemsMatched  int `json:"epgItemsMatched"`
	TimersAttempted  int `json:"timersAttempted"`
	TimersCreated    int `json:"timersCreated"`
	TimersSkipped    int `json:"timersSkipped"` // duplicates/filtered/limit
	TimersConflicted int `json:"timersConflicted"`
	TimersErrored    int `json:"timersErrored"`

	// Guardrail telemetry
	MaxTimersGlobalPerRunHit    bool `json:"maxTimersGlobalPerRunHit"`
	MaxMatchesScannedPerRuleHit bool `json:"maxMatchesScannedPerRuleHit"`
	ReceiverUnreachable         bool `json:"receiverUnreachable"`
}

// RunDecision explains why a specific action was taken for a potential match.
type RunDecision struct {
	ServiceRef  string   `json:"serviceRef"`
	Begin       int64    `json:"begin"`
	End         int64    `json:"end"`
	Title       string   `json:"title,omitempty"`
	Action      string   `json:"action"`      // "created" | "skipped" | "conflict" | "error"
	Reason      string   `json:"reason"`      // "duplicate" | "limit" | "no_match" | "day_mismatch" | "window_mismatch" | "overlap" | "receiver_error"
	MatchReason []string `json:"matchReason"` // e.g. ["keyword match", "day match"]
	TimerID     string   `json:"timerId,omitempty"`
	Details     string   `json:"details,omitempty"`
}

// RunError captures a non-fatal error encountered during the run.
type RunError struct {
	Type      string    `json:"type"` // "epg_parse" | "receiver_call" | "unexpected"
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
	Retryable bool      `json:"retryable"`
}

// RunConflict details a detected overlap that prevented scheduling.
type RunConflict struct {
	ServiceRef      string `json:"serviceRef"`
	Begin           int64  `json:"begin"`
	End             int64  `json:"end"`
	Title           string `json:"title,omitempty"`
	BlockingTimerID string `json:"blockingTimerId,omitempty"`
	OverlapSeconds  int64  `json:"overlapSeconds,omitempty"`
	Message         string `json:"message,omitempty"`
}

const (
	ActionCreated  = "created"
	ActionSkipped  = "skipped"
	ActionConflict = "conflict"
	ActionError    = "error"
)
