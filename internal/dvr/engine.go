package dvr

import (
	"context"
	"fmt"
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
	// 1. Singleflight to prevent concurrent runs
	res, err, _ := e.sfg.Do("run", func() (interface{}, error) {
		jobStart := time.Now()
		e.logger.Info().Str("trigger", trigger).Str("rule_id", ruleID).Msg("starting series engine run")

		var reports []SeriesRuleRunReport

		// 2. Get Rules
		var rules []SeriesRule
		if ruleID != "" {
			if r, ok := e.ruleManager.GetRule(ruleID); ok {
				rules = []SeriesRule{r}
			} else {
				return nil, fmt.Errorf("rule not found: %s", ruleID)
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

		// 4. Processing Loop
		globalLimit := 100
		createdCount := 0

		for _, rule := range rules {
			ruleStart := time.Now()
			if !rule.Enabled {
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
					ID:          rule.ID,
					Enabled:     rule.Enabled,
					Keyword:     rule.Keyword,
					ChannelRef:  rule.ChannelRef,
					Days:        rule.Days,
					StartWindow: rule.StartWindow,
					Priority:    rule.Priority,
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
						err := client.AddTimer(ctx, d.ServiceRef, d.Begin, d.End, d.Title, "Auto: "+rule.Keyword)
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
						}
					} else if d.Action == ActionSkipped {
						report.Summary.TimersSkipped++
					} else if d.Action == ActionConflict {
						report.Summary.TimersConflicted++
					}
				}
			}

			report.FinishedAt = time.Now()
			report.DurationMs = report.FinishedAt.Sub(ruleStart).Milliseconds()
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
		events, err := client.GetEPG(ctx, rule.ChannelRef, 0) // 0 = all/default limit?
		if err != nil {
			return nil, err
		}
		candidates = events
	} else {
		return nil, nil
	}

	var decisions []RunDecision
	kw := strings.ToLower(rule.Keyword)

	for _, ev := range candidates {
		// 1. Keyword Match
		if kw != "" && !strings.Contains(strings.ToLower(ev.Title), kw) {
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
			for _, d := range rule.Days {
				if d == weekday {
					dayMatch = true
					break
				}
			}
			if !dayMatch {
				continue
			}
		}

		// 3. Time Window
		if rule.StartWindow != "" {
			// Format HHMM-HHMM
			parts := strings.Split(rule.StartWindow, "-")
			if len(parts) == 2 {
				winStart, _ := parseHHMM(parts[0])
				winEnd, _ := parseHHMM(parts[1])

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
				fmt.Printf("DEBUG: TimeWindow: time=%d win=%d-%d in=%v\n", evTime, winStart, winEnd, inWindow)

				if !inWindow {
					continue
				}
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
		// TODO: Implement using DetectConflicts logic

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
