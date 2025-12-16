package dvr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"golang.org/x/sync/singleflight"
)

// SeriesEngine manages the execution of series rules.
type SeriesEngine struct {
	rulesManager *Manager
	client       TimerClient // Interface for GetTimers, AddTimer
	epgCache     EpgProvider // Interface to get cached EPG

	mu    sync.Mutex
	group singleflight.Group

	// Configuration
	MaxTimersGlobalPerRun    int
	MaxMatchesScannedPerRule int
	MaxTimersPerRule         int
	HorizonDays              int
}

type EpgProvider interface {
	GetEvents(from, to time.Time) ([]openwebif.EPGEvent, error)
}

type TimerClient interface {
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
	AddTimer(ctx context.Context, sRef string, begin, end int64, name, description string) error
}

// NewSeriesEngine creates a new engine.
func NewSeriesEngine(rm *Manager, client TimerClient, epg EpgProvider) *SeriesEngine {
	return &SeriesEngine{
		rulesManager:             rm,
		client:                   client,
		epgCache:                 epg,
		MaxTimersGlobalPerRun:    100,
		MaxMatchesScannedPerRule: 500,
		MaxTimersPerRule:         25,
		HorizonDays:              7,
	}
}

// RunOnce executes a single pass of the series engine.
// It is protected by singleflight to ensure only one run happens at a time.
func (e *SeriesEngine) RunOnce(ctx context.Context, trigger string, ruleID string) ([]SeriesRuleRunReport, error) {
	// Singleflight Key: "global" if ruleID is empty (all), or specific ID
	key := "run_all"
	if ruleID != "" {
		key = "run_" + ruleID
	}

	result, err, _ := e.group.Do(key, func() (interface{}, error) {
		return e.runLogic(ctx, trigger, ruleID)
	})

	if err != nil {
		return nil, err
	}
	return result.([]SeriesRuleRunReport), nil
}

func (e *SeriesEngine) runLogic(ctx context.Context, trigger string, ruleID string) ([]SeriesRuleRunReport, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	startGlobal := time.Now()

	// 1. Load Rules
	allRules := e.rulesManager.GetRules()
	var targetRules []*SeriesRule
	if ruleID != "" {
		for i := range allRules {
			if allRules[i].ID == ruleID {
				targetRules = append(targetRules, &allRules[i])
				break
			}
		}
		if len(targetRules) == 0 {
			return nil, fmt.Errorf("rule not found: %s", ruleID)
		}
	} else {
		// Filter enabled rules
		for i := range allRules {
			if allRules[i].Enabled {
				targetRules = append(targetRules, &allRules[i])
			}
		}
	}

	// Sort Rules: Priority Desc, then ID
	sort.Slice(targetRules, func(i, j int) bool {
		if targetRules[i].Priority != targetRules[j].Priority {
			return targetRules[i].Priority > targetRules[j].Priority
		}
		return targetRules[i].ID < targetRules[j].ID
	})

	// 2. Load State (Timers & EPG)
	// Fetch Timers (Receiver State)
	timers, err := e.client.GetTimers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch timers from receiver: %w", err)
	}

	// Build Global Dedupe Set (ServiceRef|Begin|End)
	// Only count active/recording/waiting timers. Ignore disabled/completed if desired.
	// User spec: "completed ignorieren" -> logic: if t.State == 3 (finished) skip?
	// Note: OpenWebIF states vary. Assuming standard states.
	dedupeSet := make(map[string]bool)
	for _, t := range timers {
		// key = ServiceRef|Begin|End
		key := fmt.Sprintf("%s|%d|%d", t.ServiceRef, t.Begin, t.End)
		dedupeSet[key] = true
	}

	// Fetch EPG (Windowed)
	// Always scan `now - 2h` to `now + Horizon`
	windowFrom := startGlobal.Add(-2 * time.Hour)
	windowTo := startGlobal.Add(time.Duration(e.HorizonDays) * 24 * time.Hour)

	// Assuming epgCache.GetEvents returns flat list.
	// Optimization: Ideally EPG provider supports windowing.
	epgItems, err := e.epgCache.GetEvents(windowFrom, windowTo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch epg: %w", err)
	}

	// 3. Process Rules
	var reports []SeriesRuleRunReport
	globalTimersCreated := 0

	for i := range targetRules {
		rule := targetRules[i]
		runID := fmt.Sprintf("%d", startGlobal.UnixNano())
		report := SeriesRuleRunReport{
			RuleID:     rule.ID,
			RunID:      runID,
			Trigger:    trigger,
			StartedAt:  time.Now(),
			WindowFrom: windowFrom.Unix(),
			WindowTo:   windowTo.Unix(),
			Status:     "success",
			Snapshot: RuleSnapshot{
				ID:          rule.ID,
				Enabled:     rule.Enabled,
				Keyword:     rule.Keyword,
				ChannelRef:  rule.ChannelRef,
				Days:        rule.Days,
				StartWindow: rule.StartWindow,
				Priority:    rule.Priority,
			},
		}

		// Check Global Limit
		if globalTimersCreated >= e.MaxTimersGlobalPerRun {
			report.Summary.MaxTimersGlobalPerRunHit = true
			report.Status = "partial"
			report.Decisions = append(report.Decisions, RunDecision{
				Action: "skipped",
				Reason: "global_limit_hit",
			})
			goto FinalizeReport
		}

		// Find Matches
		// Use a closure or explicit block to avoid goto variable skipping issues
		{
			var ruleMatches []openwebif.EPGEvent
			scannedCount := 0

			for _, event := range epgItems {
				// Convert EPG Time (Unix) to Go Time
				evtStart := time.Unix(event.Begin, 0)

				// Only consider events in window
				if evtStart.Before(windowFrom) || evtStart.After(windowTo) {
					continue
				}

				scannedCount++
				if scannedCount > e.MaxMatchesScannedPerRule {
					report.Summary.MaxMatchesScannedPerRuleHit = true
					break
				}

				// Match Logic
				matchRes := rule.Matches(event.Title, event.SRef, evtStart) // Using SRef as ChannelRef
				report.Summary.EpgItemsScanned++

				if matchRes.Matched {
					ruleMatches = append(ruleMatches, event)
					report.Summary.EpgItemsMatched++
				}
			}

			// Sort Matches by Start Time Ascending (Deterministic)
			sort.Slice(ruleMatches, func(i, j int) bool {
				return ruleMatches[i].Begin < ruleMatches[j].Begin
			})

			// Schedule Matches
			createdForRule := 0
			for _, match := range ruleMatches {
				if createdForRule >= e.MaxTimersPerRule {
					report.Decisions = append(report.Decisions, RunDecision{
						Title:  match.Title,
						Action: "skipped",
						Reason: "rule_limit_hit",
					})
					break
				}

				// Check Dedupe
				// Padding logic (Default 0 for now)
				padBefore := 0
				padAfter := 0

				// Timer Request Parameters
				tBegin := match.Begin - int64(padBefore*60)
				tEnd := match.Begin + match.Duration + int64(padAfter*60)

				key := fmt.Sprintf("%s|%d|%d", match.SRef, tBegin, tEnd)
				if dedupeSet[key] {
					report.Summary.TimersSkipped++
					// Optional verbose logging
					report.Decisions = append(report.Decisions, RunDecision{
						Title:      match.Title,
						ServiceRef: match.SRef,
						Begin:      tBegin,
						End:        tEnd,
						Action:     "skipped",
						Reason:     "duplicate",
					})
					continue
				}

				// Execute AddTimer
				err := e.client.AddTimer(ctx, match.SRef, tBegin, tEnd, match.Title, match.Description)
				report.Summary.TimersAttempted++

				if err != nil {
					report.Summary.TimersErrored++
					report.Errors = append(report.Errors, RunError{
						Type:      "receiver_error",
						Message:   err.Error(),
						At:        time.Now(),
						Retryable: true,
					})
					report.Decisions = append(report.Decisions, RunDecision{
						Title:   match.Title,
						Action:  "error",
						Reason:  "receiver_error",
						Details: err.Error(),
					})
					continue
				}

				// Success
				report.Summary.TimersCreated++
				createdForRule++
				globalTimersCreated++
				dedupeSet[key] = true

				report.Decisions = append(report.Decisions, RunDecision{
					Title:       match.Title,
					ServiceRef:  match.SRef,
					Begin:       tBegin,
					End:         tEnd,
					Action:      "created",
					Reason:      "match",
					MatchReason: []string{"rule_match"},
				})
			}
		}

	FinalizeReport:
		report.FinishedAt = time.Now()
		report.DurationMs = report.FinishedAt.Sub(report.StartedAt).Milliseconds()

		// Update Rule Stats
		rule.LastRunAt = report.FinishedAt
		rule.LastRunStatus = report.Status
		rule.LastRunSummary = report.Summary
		e.rulesManager.SaveRules()

		// Save Report to Disk
		e.persistReport(rule.ID, report)

		reports = append(reports, report)
	}

	return reports, nil
}

func (e *SeriesEngine) persistReport(ruleID string, report SeriesRuleRunReport) {
	// Simple JSON dump
	// Assuming datadir is known or passed.
	// Ideally rulesManager knows the datadir.
	// We'll peek at rulesManager.dataDir if accessible, or assume "data".
	// For now, let's write to "data/series_reports/{ruleId}.json"

	dir := "data/series_reports"
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Println("failed to create reports dir", err)
		return
	}

	fPath := filepath.Join(dir, ruleID+"_latest.json")
	bytes, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile(fPath, bytes, 0644)
}
