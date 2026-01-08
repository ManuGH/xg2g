// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/dvr"
)

// GetSeriesRules implements ServerInterface
func (s *Server) GetSeriesRules(w http.ResponseWriter, r *http.Request) {
	rules := s.seriesManager.GetRules()

	resp := make([]SeriesRule, 0, len(rules))
	for _, rule := range rules {
		resp = append(resp, mapRuleToAPI(rule))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateSeriesRule implements ServerInterface
func (s *Server) CreateSeriesRule(w http.ResponseWriter, r *http.Request) {
	var req SeriesRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid JSON", "INVALID_INPUT", "The request body could not be decoded as JSON", nil)
		return
	}

	// Validate keyword
	if req.Keyword == nil || strings.TrimSpace(*req.Keyword) == "" {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Keyword Required", "INVALID_INPUT", "The rule keyword cannot be empty", nil)
		return
	}

	dvrRule := mapAPIToRule(req)

	// Persist
	id, err := s.seriesManager.AddRule(dvrRule)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "dvr/save_failed", "Save Failed", "SAVE_FAILED", "Failed to create rule", nil)
		return
	}

	// Retrieve created rule with server-managed fields
	created, ok := s.seriesManager.GetRule(id)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "dvr/not_found", "Rule Not Found", "NOT_FOUND", "Rule not found after creation", nil)
		return
	}

	resp := mapRuleToAPI(created)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// UpdateSeriesRule implements ServerInterface
func (s *Server) UpdateSeriesRule(w http.ResponseWriter, r *http.Request, id string) {
	var req SeriesRuleUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Invalid JSON", "INVALID_INPUT", "The request body could not be decoded as JSON", nil)
		return
	}

	// Validate keyword (already required by schema, but check for empty)
	if strings.TrimSpace(req.Keyword) == "" {
		writeProblem(w, r, http.StatusBadRequest, "dvr/invalid_input", "Keyword Required", "INVALID_INPUT", "The rule keyword cannot be empty", nil)
		return
	}

	dvrRule := mapAPIUpdateToRule(req)

	if err := s.seriesManager.UpdateRule(id, dvrRule); err != nil {
		if errors.Is(err, dvr.ErrRuleNotFound) {
			writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Rule Not Found", "NOT_FOUND", "The specified rule does not exist", nil)
			return
		}
		writeProblem(w, r, http.StatusInternalServerError, "dvr/save_failed", "Save Failed", "SAVE_FAILED", "Failed to update rule", nil)
		return
	}

	// Retrieve updated rule with server-managed fields
	updated, ok := s.seriesManager.GetRule(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Rule Not Found", "NOT_FOUND", "The specified rule does not exist", nil)
		return
	}

	resp := mapRuleToAPI(updated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteSeriesRule implements ServerInterface
func (s *Server) DeleteSeriesRule(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.seriesManager.DeleteRule(id); err != nil {
		if errors.Is(err, dvr.ErrRuleNotFound) {
			writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Rule Not Found", "NOT_FOUND", "The specified rule does not exist", nil)
			return
		}
		writeProblem(w, r, http.StatusInternalServerError, "dvr/save_failed", "Save Failed", "SAVE_FAILED", "Failed to delete rule", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Helpers
func mapRuleToAPI(r dvr.SeriesRule) SeriesRule {
	// Create deep copies for slices if needed, but for json marshalling it's fine.
	// We need to take address of local variables or fields.
	// But taking address of field in loop var 'r' (if it was range var) is risky locally,
	// but here 'r' is value argument. Address of fields is safe?
	// Actually no, escaping reference to stack.
	// Safe to create new variables.

	id := r.ID
	enabled := r.Enabled
	keyword := r.Keyword
	channelRef := r.ChannelRef
	days := r.Days
	window := r.StartWindow
	priority := r.Priority

	// Map LastRunSummary if present
	var summary *RunSummary
	if r.LastRunSummary.EpgItemsScanned > 0 || r.LastRunSummary.TimersAttempted > 0 || r.LastRunStatus != "" {
		// Populate only if meaningful data exists
		s := RunSummary{
			EpgItemsScanned:             &r.LastRunSummary.EpgItemsScanned,
			EpgItemsMatched:             &r.LastRunSummary.EpgItemsMatched,
			TimersAttempted:             &r.LastRunSummary.TimersAttempted,
			TimersCreated:               &r.LastRunSummary.TimersCreated,
			TimersSkipped:               &r.LastRunSummary.TimersSkipped,
			TimersConflicted:            &r.LastRunSummary.TimersConflicted,
			TimersErrored:               &r.LastRunSummary.TimersErrored,
			MaxTimersGlobalPerRunHit:    &r.LastRunSummary.MaxTimersGlobalPerRunHit,
			MaxMatchesScannedPerRuleHit: &r.LastRunSummary.MaxMatchesScannedPerRuleHit,
			ReceiverUnreachable:         &r.LastRunSummary.ReceiverUnreachable,
		}
		summary = &s
	}

	return SeriesRule{
		Id:             &id,
		Enabled:        &enabled,
		Keyword:        &keyword,
		ChannelRef:     &channelRef,
		Days:           &days,
		StartWindow:    &window,
		Priority:       &priority,
		LastRunAt:      &r.LastRunAt,
		LastRunStatus:  &r.LastRunStatus,
		LastRunSummary: summary,
	}
}

func mapAPIToRule(req SeriesRule) dvr.SeriesRule {
	r := dvr.SeriesRule{}
	if req.Id != nil {
		r.ID = *req.Id
	}
	if req.Enabled != nil {
		r.Enabled = *req.Enabled
	}
	if req.Keyword != nil {
		r.Keyword = *req.Keyword
	}
	if req.ChannelRef != nil {
		r.ChannelRef = *req.ChannelRef
	}
	if req.Days != nil {
		r.Days = *req.Days
	}
	if req.StartWindow != nil {
		r.StartWindow = *req.StartWindow
	}
	if req.Priority != nil {
		r.Priority = *req.Priority
	}
	return r
}

func mapAPIUpdateToRule(req SeriesRuleUpdate) dvr.SeriesRule {
	r := dvr.SeriesRule{}
	// Required fields (non-pointer)
	r.Enabled = req.Enabled
	r.Keyword = req.Keyword
	r.Priority = req.Priority

	// Optional fields (pointer)
	if req.ChannelRef != nil {
		r.ChannelRef = *req.ChannelRef
	}
	if req.Days != nil {
		r.Days = *req.Days
	}
	if req.StartWindow != nil {
		r.StartWindow = *req.StartWindow
	}
	return r
}

// RunAllSeriesRules implements ServerInterface
func (s *Server) RunAllSeriesRules(w http.ResponseWriter, r *http.Request, params RunAllSeriesRulesParams) {
	trigger := "manual"
	if params.Trigger != nil {
		trigger = *params.Trigger
	}

	reports, err := s.seriesEngine.RunOnce(r.Context(), trigger, "")
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "dvr/engine_error", "Run Failed", "ENGINE_ERROR", err.Error(), nil)
		return
	}

	apiReports := make([]SeriesRuleRunReport, 0, len(reports))
	for _, rep := range reports {
		apiReports = append(apiReports, mapReportToAPI(rep))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiReports)
}

// RunSeriesRule implements ServerInterface
func (s *Server) RunSeriesRule(w http.ResponseWriter, r *http.Request, id string, params RunSeriesRuleParams) {
	trigger := "manual"
	if params.Trigger != nil {
		trigger = *params.Trigger
	}

	reports, err := s.seriesEngine.RunOnce(r.Context(), trigger, id)
	if err != nil {
		if err.Error() == "rule not found: "+id {
			writeProblem(w, r, http.StatusNotFound, "dvr/not_found", "Rule Not Found", "NOT_FOUND", "The specified rule does not exist", nil)
			return
		}
		writeProblem(w, r, http.StatusInternalServerError, "dvr/engine_error", "Run Failed", "ENGINE_ERROR", err.Error(), nil)
		return
	}

	if len(reports) == 0 {
		writeProblem(w, r, http.StatusInternalServerError, "dvr/engine_error", "Run Failed", "ENGINE_ERROR", "No report generated", nil)
		return
	}

	apiReport := mapReportToAPI(reports[0])

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(apiReport)
}

func mapReportToAPI(r dvr.SeriesRuleRunReport) SeriesRuleRunReport {
	// Map Summary
	summary := RunSummary{
		EpgItemsScanned:             &r.Summary.EpgItemsScanned,
		EpgItemsMatched:             &r.Summary.EpgItemsMatched,
		TimersAttempted:             &r.Summary.TimersAttempted,
		TimersCreated:               &r.Summary.TimersCreated,
		TimersSkipped:               &r.Summary.TimersSkipped,
		TimersConflicted:            &r.Summary.TimersConflicted,
		TimersErrored:               &r.Summary.TimersErrored,
		MaxTimersGlobalPerRunHit:    &r.Summary.MaxTimersGlobalPerRunHit,
		MaxMatchesScannedPerRuleHit: &r.Summary.MaxMatchesScannedPerRuleHit,
		ReceiverUnreachable:         &r.Summary.ReceiverUnreachable,
	}

	// Map Snapshot
	snap := RuleSnapshot{
		Id:          &r.Snapshot.ID,
		Enabled:     &r.Snapshot.Enabled,
		Keyword:     &r.Snapshot.Keyword,
		ChannelRef:  &r.Snapshot.ChannelRef,
		Days:        &r.Snapshot.Days,
		StartWindow: &r.Snapshot.StartWindow,
		Priority:    &r.Snapshot.Priority,
	}

	// Map Decisions
	decisions := make([]RunDecision, 0, len(r.Decisions))
	for _, d := range r.Decisions {
		decisions = append(decisions, RunDecision{
			ServiceRef:  &d.ServiceRef,
			Begin:       &d.Begin,
			End:         &d.End,
			Title:       &d.Title,
			Action:      &d.Action,
			Reason:      &d.Reason,
			MatchReason: &d.MatchReason,
			TimerId:     &d.TimerID,
			Details:     &d.Details,
		})
	}

	// Map Errors
	errors := make([]RunError, 0, len(r.Errors))
	for _, e := range r.Errors {
		errors = append(errors, RunError{
			Type:      &e.Type,
			Message:   &e.Message,
			At:        &e.At,
			Retryable: &e.Retryable,
		})
	}

	// Map Conflicts
	conflicts := make([]RunConflict, 0, len(r.Conflicts))
	for _, c := range r.Conflicts {
		conflicts = append(conflicts, RunConflict{
			ServiceRef:      &c.ServiceRef,
			Begin:           &c.Begin,
			End:             &c.End,
			Title:           &c.Title,
			BlockingTimerId: &c.BlockingTimerID,
			OverlapSeconds:  &c.OverlapSeconds,
			Message:         &c.Message,
		})
	}

	statusStr := string(r.Status)

	return SeriesRuleRunReport{
		RuleId:     &r.RuleID,
		RunId:      &r.RunID,
		Trigger:    &r.Trigger,
		StartedAt:  &r.StartedAt,
		FinishedAt: &r.FinishedAt,
		DurationMs: &r.DurationMs,
		WindowFrom: &r.WindowFrom,
		WindowTo:   &r.WindowTo,
		Status:     (*SeriesRuleRunReportStatus)(&statusStr),
		Summary:    &summary,
		Snapshot:   &snap,
		Decisions:  &decisions,
		Errors:     &errors,
		Conflicts:  &conflicts,
	}
}
