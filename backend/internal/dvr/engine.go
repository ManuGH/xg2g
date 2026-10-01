// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dvr

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
	"golang.org/x/sync/singleflight"
)

// OWIClient abstracts the OpenWebIF client for testing
type OWIClient interface {
	GetTimers(ctx context.Context) ([]openwebif.Timer, error)
	AddTimer(ctx context.Context, sRef string, begin, end int64, name, description string) error
	DeleteTimer(ctx context.Context, sRef string, begin, end int64) error
	GetEPG(ctx context.Context, ref string, limit int) ([]openwebif.EPGEvent, error)
	GetRecordings(ctx context.Context, dirname string) (*openwebif.MovieList, error)
	DeleteMovie(ctx context.Context, sRef string) error
}

// SeriesEngine handles automated rule-based recording.
type SeriesEngine struct {
	cfg           config.AppConfig // Need access to global config (EPG filters etc)
	ruleManager   *Manager
	clientFactory func() OWIClient // Abstract factory to get fresh OWI client

	mu      sync.RWMutex
	lastRun time.Time

	sfg    singleflight.Group
	logger zerolog.Logger
}

// NewSeriesEngine creates a new engine instance.
func NewSeriesEngine(cfg config.AppConfig, rules *Manager, clientFactory func() OWIClient) *SeriesEngine {
	return &SeriesEngine{
		cfg:           cfg,
		ruleManager:   rules,
		clientFactory: clientFactory,
		logger:        log.WithComponent("series_engine"),
	}
}

// RunOnce executes a single pass of the series engine.
// trigger: "manual" or "auto"
// ruleID: optional, if set only runs this specific rule
func (e *SeriesEngine) RunOnce(ctx context.Context, trigger string, ruleID string) ([]SeriesRuleRunReport, error) {
	// 1. Singleflight to prevent concurrent duplicate runs for the same execution scope
	flightKey := "all"
	if ruleID != "" {
		flightKey = "rule:" + ruleID
	}
	res, err, _ := e.sfg.Do(flightKey, func() (any, error) {
		jobStart := time.Now()
		e.logger.Info().Str("trigger", trigger).Str("rule_id", ruleID).Msg("starting series engine run")

		var reports []SeriesRuleRunReport

		// 2. Get Rules
		var rules []SeriesRule
		if ruleID != "" {
			if r, ok := e.ruleManager.GetRule(ruleID); ok {
				rules = []SeriesRule{r}
			} else {
				return nil, fmt.Errorf("%w: %s", ErrRuleNotFound, ruleID)
			}
		} else {
			rules = e.ruleManager.GetRules()
			// Sort by priority desc
			sort.Slice(rules, func(i, j int) bool {
				return rules[i].Priority > rules[j].Priority
			})
		}

		// 3. Get OWI Client & Current Timers (for dedup)
		client := e.clientFactory()

		// Fetch Timers (Receiver State)
		timers, err := client.GetTimers(ctx)
		if err != nil {
			e.logger.Error().Err(err).Msg("failed to fetch timers from receiver, aborting run")
			// Return empty reports or error?
			return nil, err
		}

		// Build Deduplication Map (ServiceRef + StartTime -> Exists)
		// We fuzzy match time +/- 60s
		existingTimers := make(map[string]bool)
		for _, t := range timers {
			key := fmt.Sprintf("%s|%d", t.ServiceRef, t.Begin)
			existingTimers[key] = true
		}

		// 3b. Fetch Recordings if any rule has retention configured
		var movies []openwebif.Movie
		hasRetentionRules := false
		for _, r := range rules {
			if r.Enabled && r.RetentionDays > 0 {
				hasRetentionRules = true
				break
			}
		}
		if hasRetentionRules {
			movieList, err := client.GetRecordings(ctx, "")
			if err != nil {
				e.logger.Warn().Err(err).Msg("failed to fetch recordings from receiver for retention enforcement")
			} else if movieList != nil {
				movies = movieList.Movies
			}
		}

		// 4. Processing Loop
		globalLimit := 100
		createdCount := 0

		for _, rule := range rules {
			ruleStart := time.Now()
			if !rule.Enabled && ruleID == "" {
				continue
			}

			// Init Report for this rule
			report := SeriesRuleRunReport{
				RuleID:    rule.ID,
				RunID:     fmt.Sprintf("%d-%s", jobStart.Unix(), rule.ID),
				Trigger:   trigger,
				StartedAt: ruleStart,
				Status:    "success",
				Snapshot: RuleSnapshot{
					ID:            rule.ID,
					Enabled:       rule.Enabled,
					Keyword:       rule.Keyword,
					ChannelRef:    rule.ChannelRef,
					Days:          rule.Days,
					StartWindow:   rule.StartWindow,
					Priority:      rule.Priority,
					RetentionDays: rule.RetentionDays,
				},
			}

			// Run Rule Logic
			decisions, err := e.processRule(ctx, client, rule, existingTimers)
			if err != nil {
				e.logger.Error().Err(err).Str("rule_id", rule.ID).Msg("failed to process rule")
				report.Status = "failed"
				report.Summary.TimersErrored++
				report.Errors = append(report.Errors, RunError{
					Type:    "processing",
					Message: err.Error(),
					At:      time.Now(),
				})
			} else {
				report.Decisions = decisions
				report.Summary.EpgItemsMatched = len(decisions)
				// Apply Decisions (Create Timers)
				for _, d := range decisions {
					if d.Action == ActionCreated {
						if createdCount >= globalLimit {
							report.Summary.MaxTimersGlobalPerRunHit = true
							break
						}

						e.logger.Info().Str("title", d.Title).Str("rule", rule.Keyword).Msg("scheduling timer")

						// Real Create Call
						ruleTag := fmt.Sprintf("[xg2g-rule:%s]", rule.ID)
						timerDesc := fmt.Sprintf("%s Auto: %s", ruleTag, rule.Keyword)
						err := client.AddTimer(ctx, d.ServiceRef, d.Begin, d.End, d.Title, timerDesc)
						if err != nil {
							e.logger.Error().Err(err).Msg("failed to add timer")
							report.Summary.TimersErrored++
							report.Errors = append(report.Errors, RunError{
								Type:    "receiver_add_timer",
								Message: err.Error(),
								At:      time.Now(),
							})
						} else {
							createdCount++
							report.Summary.TimersCreated++
							// Update local dedup cache to prevent double booking in same run
							key := fmt.Sprintf("%s|%d", d.ServiceRef, d.Begin)
							existingTimers[key] = true

							// Record verified ownership in manager
							if e.ruleManager != nil {
								_ = e.ruleManager.RecordOwnership(RuleRecordingOwnership{
									RuleID:     rule.ID,
									ChannelRef: d.ServiceRef,
									Begin:      d.Begin,
									End:        d.End,
									Title:      d.Title,
									CreatedAt:  time.Now(),
								})
							}
						}
					} else if d.Action == ActionSkipped {
						report.Summary.TimersSkipped++
					} else if d.Action == ActionConflict {
						report.Summary.TimersConflicted++
					}
				}

				// Apply Retention Policy (Prune expired recordings)
				if rule.RetentionDays > 0 && len(movies) > 0 {
					pruned, pruneDecisions := e.pruneRecordingsForRule(ctx, client, rule, jobStart, movies)
					report.Summary.RecordingsPruned = pruned
					report.Decisions = append(report.Decisions, pruneDecisions...)
				}
			}

			report.FinishedAt = time.Now()
			report.DurationMs = report.FinishedAt.Sub(ruleStart).Milliseconds()
			rule.LastRunAt = report.FinishedAt
			rule.LastRunStatus = report.Status
			rule.LastRunSummary = report.Summary
			_ = e.ruleManager.UpdateRule(rule.ID, rule)
			reports = append(reports, report)
		}

		e.mu.Lock()
		e.lastRun = time.Now()
		e.mu.Unlock()

		return reports, nil
	})

	if err != nil {
		return nil, err
	}
	return res.([]SeriesRuleRunReport), nil
}

// processRule matches a single rule against the EPG
func (e *SeriesEngine) processRule(ctx context.Context, client OWIClient, rule SeriesRule, existingTimers map[string]bool) ([]RunDecision, error) {
	// Load EPG (Service or Global?)
	// If rule has ChannelRef, only fetch EPG for that channel.
	// If not, fetch Global EPG? (Expensive! Maybe limit to Bouquet?)
	// Implementation Plan says: "Scan Limit: 500".

	// For efficiency, if no ChannelRef, we might skip global scan for now or warn.
	// Or we use client.GetEPG("") which might not work well on all boxes.
	// Let's assume we iterate configured bouquet for now if ChannelRef empty?

	var candidates []openwebif.EPGEvent

	if rule.ChannelRef != "" {
		// Focused Scan
		days := 7
		if rule.RetentionDays > 0 && rule.RetentionDays <= 14 {
			days = rule.RetentionDays
		}
		events, err := client.GetEPG(ctx, rule.ChannelRef, days)
		if err != nil {
			return nil, err
		}
		candidates = events
	} else {
		return nil, fmt.Errorf("series rule %q has no channelRef: scanning all channels is not supported, channelRef is required", rule.ID)
	}

	var decisions []RunDecision
	kw := NormalizeForMatch(rule.Keyword)

	// Parse the optional StartWindow ONCE (it is constant for the rule). A malformed window
	// fails the rule run loudly — RunOnce records a per-rule "failed" — instead of silently
	// matching with parseHHMM's -1 sentinel, which recorded the wrong programs (or nothing)
	// while the run reported success. Parsing here (not per event) also avoids a log/error
	// flood across the candidate loop.
	winStart, winEnd, hasWindow := 0, 0, false
	if rule.StartWindow != "" {
		parts := strings.Split(rule.StartWindow, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid series rule StartWindow %q: expected HHMM-HHMM", rule.StartWindow)
		}
		var errStart, errEnd error
		if winStart, errStart = parseHHMM(parts[0]); errStart != nil {
			return nil, fmt.Errorf("invalid series rule StartWindow %q: %w", rule.StartWindow, errStart)
		}
		if winEnd, errEnd = parseHHMM(parts[1]); errEnd != nil {
			return nil, fmt.Errorf("invalid series rule StartWindow %q: %w", rule.StartWindow, errEnd)
		}
		hasWindow = true
	}

	for _, ev := range candidates {
		// 1. Keyword Match
		if kw != "" && !strings.Contains(NormalizeForMatch(ev.Title), kw) {
			continue // No match
		}

		start := time.Unix(ev.Begin, 0) // Local time by default in Go if system set? No, .Unix() is UTC.
		// Enigma2 EPG is usually UTC unix timestamp.
		// Rules are created in local time (e.g. "20:15").
		// We need to compare in the configured location or Local.
		// For simplicity, assume server timezone matches user timezone for now (Local).
		localStart := start.Local()

		// 2. Day Filter
		if len(rule.Days) > 0 {
			dayMatch := false
			weekday := int(localStart.Weekday()) // 0=Sunday
			if slices.Contains(rule.Days, weekday) {
				dayMatch = true
			}
			if !dayMatch {
				continue
			}
		}

		// 3. Time Window (parsed once above into winStart/winEnd)
		if hasWindow {
			evTime := localStart.Hour()*100 + localStart.Minute()

			inWindow := false
			if winStart <= winEnd {
				// Normal window (e.g. 1000-1200)
				if evTime >= winStart && evTime <= winEnd {
					inWindow = true
				}
			} else {
				// Wrap-around window (e.g. 2200-0200)
				if evTime >= winStart || evTime <= winEnd {
					inWindow = true
				}
			}

			if !inWindow {
				continue
			}
		}

		// 4. Duplicate Check
		// Check global dedup (existing timers)
		key := fmt.Sprintf("%s|%d", ev.SRef, ev.Begin)
		if existingTimers[key] {
			decisions = append(decisions, RunDecision{
				ServiceRef: ev.SRef,
				Begin:      ev.Begin,
				Title:      ev.Title,
				Action:     ActionSkipped,
				Reason:     "duplicate",
			})
			continue
		}

		// 4b. Self-Duplicate Check within this run (prevent scheduling same event twice if EPG has dupes?)
		// Already handled by caching decision in 'existingTimers' in the main loop if we created it.
		// But here we are just collecting decisions.
		// We'll rely on the main loop to check 'existingTimers' again BEFORE executing if we want strictly safe?
		// No, main loop uses key.

		// 5. Conflict Check (Placeholder)
		// NOTE: Conflict detection intentionally deferred (see docs/TROUBLESHOOTING.md)
		// Future work: Implement DetectConflicts logic that accounts for tuner count and existing timers

		// Success
		decisions = append(decisions, RunDecision{
			ServiceRef:  ev.SRef,
			Begin:       ev.Begin,
			End:         ev.Begin + ev.Duration,
			Title:       ev.Title,
			Action:      ActionCreated,
			MatchReason: []string{"keyword"},
		})
	}

	return decisions, nil
}

// parseHHMM parses "HHMM" string to int (e.g. "2015" -> 2015). Returns -1 on error.
func parseHHMM(s string) (int, error) {
	s = strings.ReplaceAll(s, ":", "")
	if len(s) != 4 {
		return -1, fmt.Errorf("invalid length")
	}
	var val int
	_, err := fmt.Sscanf(s, "%d", &val)
	if err != nil {
		return -1, err
	}
	return val, nil
}

// isRecordingOwnedByRule verifies if a recording belongs to ruleID either via
// embedded description tag [xg2g-rule:<ruleID>] or via persistent scheduled ownership record.
func (e *SeriesEngine) isRecordingOwnedByRule(ruleID string, ruleChannelRef string, movie openwebif.Movie) bool {
	ruleTag := fmt.Sprintf("[xg2g-rule:%s]", ruleID)
	if strings.Contains(movie.Description, ruleTag) || strings.Contains(movie.ExtendedDescription, ruleTag) {
		return true
	}
	if e.ruleManager != nil && e.ruleManager.HasOwnership(ruleID, movie.Title, int64(movie.Begin), ruleChannelRef) {
		return true
	}
	return false
}

// pruneRecordingsForRule checks existing recordings against rule.RetentionDays and deletes any that are expired
// and verified to be owned by this rule.
func (e *SeriesEngine) pruneRecordingsForRule(ctx context.Context, client OWIClient, rule SeriesRule, now time.Time, movies []openwebif.Movie) (int, []RunDecision) {
	if rule.RetentionDays <= 0 || len(movies) == 0 {
		return 0, nil
	}

	cutoff := now.Add(-time.Duration(rule.RetentionDays) * 24 * time.Hour)
	normKw := NormalizeForMatch(rule.Keyword)
	prunedCount := 0
	var decisions []RunDecision

	for _, movie := range movies {
		// 1. Keyword match on recording title
		if normKw != "" && !strings.Contains(NormalizeForMatch(movie.Title), normKw) {
			continue
		}

		// 2. Channel match if rule restricts to a channel
		if rule.ChannelRef != "" {
			matchesChannel := false
			if strings.EqualFold(movie.ServiceRef, rule.ChannelRef) ||
				strings.Contains(strings.ToLower(movie.ServiceRef), strings.ToLower(rule.ChannelRef)) {
				matchesChannel = true
			}
			if !matchesChannel && movie.ServiceName != "" {
				if strings.Contains(strings.ToLower(rule.ChannelRef), strings.ToLower(movie.ServiceName)) ||
					strings.Contains(strings.ToLower(movie.ServiceName), strings.ToLower(rule.ChannelRef)) {
					matchesChannel = true
				}
			}
			// If sRef is generic file path (1:0:0:0:...), allow title match to continue to ownership verification
			if !matchesChannel && !strings.HasPrefix(movie.ServiceRef, "1:0:0:0:") {
				continue
			}
		}

		// 3. Time check
		movieBegin := int64(movie.Begin)
		if movieBegin <= 0 {
			continue
		}
		recordedAt := time.Unix(movieBegin, 0)
		if recordedAt.Before(cutoff) {
			// 4. Strict Ownership Check: Must have verified ownership by this rule
			if !e.isRecordingOwnedByRule(rule.ID, rule.ChannelRef, movie) {
				e.logger.Warn().
					Str("title", movie.Title).
					Str("serviceref", movie.ServiceRef).
					Time("recorded_at", recordedAt).
					Str("rule_id", rule.ID).
					Msg("skipping retention prune: recording lacks verified rule ownership")
				decisions = append(decisions, RunDecision{
					ServiceRef: movie.ServiceRef,
					Begin:      movieBegin,
					Title:      movie.Title,
					Action:     ActionSkipped,
					Reason:     "unverified_ownership",
					Details:    fmt.Sprintf("Recording %q matches rule title but lacks verified ownership for rule %q; skipped retention deletion for safety", movie.Title, rule.ID),
				})
				continue
			}

			e.logger.Info().
				Str("title", movie.Title).
				Str("serviceref", movie.ServiceRef).
				Time("recorded_at", recordedAt).
				Int("retention_days", rule.RetentionDays).
				Str("rule_id", rule.ID).
				Msg("deleting expired recording under retention policy")

			err := client.DeleteMovie(ctx, movie.ServiceRef)
			if err != nil {
				e.logger.Error().Err(err).
					Str("title", movie.Title).
					Str("serviceref", movie.ServiceRef).
					Msg("failed to delete expired recording")
				decisions = append(decisions, RunDecision{
					ServiceRef: movie.ServiceRef,
					Begin:      movieBegin,
					Title:      movie.Title,
					Action:     ActionError,
					Reason:     "retention_delete_failed",
					Details:    err.Error(),
				})
			} else {
				prunedCount++
				decisions = append(decisions, RunDecision{
					ServiceRef: movie.ServiceRef,
					Begin:      movieBegin,
					Title:      movie.Title,
					Action:     "pruned",
					Reason:     "retention_expired",
					Details:    fmt.Sprintf("Recording older than %d days (recorded %s)", rule.RetentionDays, recordedAt.Format("2006-01-02")),
				})
			}
		}
	}

	return prunedCount, decisions
}
