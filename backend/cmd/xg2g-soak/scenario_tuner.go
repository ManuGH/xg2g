// Package main - Tuner Exhaustion + Preemption Scenario (Scenario B)
package main

import (
	"fmt"
	"time"
)

// TunerScenarioEvidence captures data for the report.
type TunerScenarioEvidence struct {
	TunerLimit       int     `json:"tuner_limit"`
	LiveSessions     int     `json:"live_sessions"`
	ActiveSessions   int     `json:"active_sessions"`
	RejectCount      int     `json:"reject_count"`
	PreemptCount     int     `json:"preempt_count"`
	PreemptedTarget  string  `json:"preempted_target_id"`
	PreemptDurationS float64 `json:"preempt_duration_s"`
	VictimStatus     int     `json:"victim_status"`
}

// runTunerExhaustionScenario executes the Tuner Preemption test.
// Logic:
// 1. Fill all tuners (maxPool/tunerLimit) with Live sessions.
// 2. Attempt Pulse (expect Reject/pool_full).
// 3. Attempt Recording (expect Admit + Preempt of a Live session).
func runTunerExhaustionScenario(cfg Config, prom *PromClient, client *SessionClient) ScenarioResult {
	result := ScenarioResult{
		Name:         "tuner_preemption",
		Pass:         true,
		Observations: make(map[string]int64),
		Failures:     []Failure{},
	}

	evidence := TunerScenarioEvidence{}
	limit := cfg.TunerCountOverride
	if limit <= 0 {
		limit = 4
	}
	evidence.TunerLimit = limit

	fmt.Printf("[Tuner] Target Limit: %d\n", limit)

	var activeSessions []string // Track all stated session IDs
	var liveIDs []string        // Track specifically Live session IDs to find victim
	defer func() {
		client.StopAllSessions(activeSessions)
	}()

	// Metric names
	// Note: We use raw metric names for construction with selector support in prom.go
	liveMetric := prom.Metric(`xg2g_active_sessions{priority="live"}`)
	recMetric := prom.Metric(`xg2g_active_sessions{priority="recording"}`)
	tunerMetric := prom.Metric("xg2g_tuners_in_use")
	preemptMetric := prom.Metric("xg2g_preempt_total")
	// Sum of active sessions - requires query construction manually wrapping selector-injected metric?
	// prom.Metric("xg2g_active_sessions") -> xg2g_active_sessions{selector}
	// We construct sum check manually if needed, or rely on active_sessions{live}==limit.
	// Since we fill with Live, sum == active{live}.

	// Baseline preemption count
	preemptBaseline, _ := prom.QueryValue(fmt.Sprintf("sum(%s)", preemptMetric))

	// ===================
	// Phase 1: Fill with Live
	// ===================
	// Assumption: Harness must use Tuner sources for Tuner Usage metric to Increment.
	// We use "tuner://..." style serviceRef if supported or plain defaults if adapter handles it.
	// We assume "soak-tuner-live-N" maps to a tuner source in the backend mapping or we pass specific ID?
	// Phase 5.3 runbook implies system under test configuration.
	// For now we assume standard Live requests consume Tuners if configured.

	fmt.Printf("[Tuner] Phase 1: Filling %d slots with Live sessions\n", limit)
	for i := 0; i < limit; i++ {
		// Use a service ref that implies tuner usage if possible.
		// If "live" priority is used, and adapter logic sees source type Tuner.
		// We rely on backend config mapping "soak-tuner-live-N" to Tuner source.
		serviceRef := fmt.Sprintf("soak-tuner-live-%d", i)
		res := client.StartSession(serviceRef, "live")
		if res.Error != nil || (res.HTTPStatus != 200 && res.HTTPStatus != 201 && res.HTTPStatus != 202) {
			result.Failures = append(result.Failures, Failure{
				Time:    time.Now(),
				RuleID:  "TUNER_FILL_FAIL",
				Message: fmt.Sprintf("Failed to fill slot %d: %d %v", i, res.HTTPStatus, res.Error),
			})
			result.Pass = false
			return result
		}
		activeSessions = append(activeSessions, res.SessionID)
		liveIDs = append(liveIDs, res.SessionID)
		evidence.LiveSessions++
	}

	// Verify Active Sessions & Tuners via Prom (Stable)
	fmt.Printf("[Tuner] Waiting for %d live sessions and tuners...\n", limit)
	if err := prom.WaitStable(liveMetric, float64(limit), 10*time.Second, 30*time.Second); err != nil {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "TUNER_METRIC_MISMATCH_LIVE",
			Message: fmt.Sprintf("Live sessions not stable at %d: %v", limit, err),
		})
		result.Pass = false
	}
	if err := prom.WaitStable(tunerMetric, float64(limit), 10*time.Second, 30*time.Second); err != nil {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "TUNER_METRIC_MISMATCH_TUNER",
			Message: fmt.Sprintf("Tuners in use not stable at %d: %v", limit, err),
		})
		result.Pass = false
	}

	if !result.Pass {
		return result
	}

	// ===================
	// Phase 2: Pulse Reject (Expect 503 Pool Full)
	// ===================
	fmt.Printf("[Tuner] Phase 2: Attempting Pulse (expect Reject)\n")
	res := client.StartSession("soak-tuner-pulse", "pulse")
	if res.HTTPStatus != 503 {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "TUNER_PULSE_ADMITTED",
			Message: fmt.Sprintf("Pulse should be rejected when full, got %d", res.HTTPStatus),
		})
		result.Pass = false
		if res.SessionID != "" {
			activeSessions = append(activeSessions, res.SessionID)
		}
	} else {
		// Verify Reason
		if res.AdmissionReason != "pool_full" && res.AdmissionReason != "tuner_busy" {
			fmt.Printf("[Tuner] Warning: Pulse rejected with reason '%s' (expected pool_full/tuner_busy)\n", res.AdmissionReason)
		}
		evidence.RejectCount++
	}

	// ===================
	// Phase 3: Recording Preemption (Expect 2XX + Kill)
	// ===================
	fmt.Printf("[Tuner] Phase 3: Attempting Recording (expect Admit + Preempt)\n")
	startPreempt := time.Now()
	resRec := client.StartSession("soak-tuner-rec", "recording")
	if resRec.HTTPStatus != 200 && resRec.HTTPStatus != 201 && resRec.HTTPStatus != 202 {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "TUNER_REC_REJECTED",
			Message: fmt.Sprintf("Recording should preempt, got reject: %d %s", resRec.HTTPStatus, resRec.AdmissionReason),
		})
		result.Pass = false
		return result
	}

	activeSessions = append(activeSessions, resRec.SessionID)
	evidence.PreemptCount++

	// Verify Metrics Post-Preempt
	// 1. Live count drops by 1
	// 2. Recording count is 1
	// 3. Tuners still full (Recording took slot)
	// 4. Preempt Total increased

	fmt.Printf("[Tuner] Verifying preemption state (Live=%d, Rec=1, Tuners=%d)...\n", limit-1, limit)

	if err := prom.WaitStable(liveMetric, float64(limit-1), 10*time.Second, 30*time.Second); err != nil {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "PREEMPT_STATE_LIVE",
			Message: fmt.Sprintf("Live sessions did not stabilize at %d: %v", limit-1, err),
		})
		result.Pass = false
	}

	if err := prom.WaitStable(recMetric, 1.0, 10*time.Second, 30*time.Second); err != nil {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "PREEMPT_STATE_REC",
			Message: fmt.Sprintf("Recording sessions did not stabilize at 1: %v", err),
		})
		result.Pass = false
	}

	if err := prom.WaitStable(tunerMetric, float64(limit), 10*time.Second, 30*time.Second); err != nil {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "PREEMPT_STATE_TUNER",
			Message: fmt.Sprintf("Tuners did not remain at limit %d: %v", limit, err),
		})
		result.Pass = false
	}

	// Verify Preemption Counter Increase
	preemptNow, valErr := prom.QueryValue(fmt.Sprintf("sum(%s)", preemptMetric))
	if valErr != nil || preemptNow <= preemptBaseline {
		// Try Delta with scalar sum
		delta, _ := prom.QueryValue(fmt.Sprintf("sum(increase(%s[2m]))", preemptMetric))
		if delta < 1 {
			result.Failures = append(result.Failures, Failure{
				Time:    time.Now(),
				RuleID:  "PREEMPT_METRIC_MISSING",
				Message: fmt.Sprintf("No preemption recorded in metrics (delta=%.0f)", delta),
			})
			result.Pass = false
		}
	}

	// Verify Victim Status (410 Gone)
	victimFound := false
	for _, sid := range liveIDs {
		status, err := client.GetSessionStatus(sid)
		if err == nil {
			if status == 410 || status == 404 {
				victimFound = true
				evidence.PreemptedTarget = sid
				evidence.VictimStatus = status
				break
			}
		}
	}

	if !victimFound {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "VICTIM_NOT_GONE",
			Message: "No Live session transitioned to 410/404 after preemption",
		})
		result.Pass = false
	}

	evidence.PreemptDurationS = time.Since(startPreempt).Seconds()

	result.Observations["live_final"] = int64(limit - 1)
	result.Observations["preempt_count"] = int64(evidence.PreemptCount)
	result.Observations["reject_count"] = int64(evidence.RejectCount)
	result.Observations["victim_status"] = int64(evidence.VictimStatus)

	fmt.Printf("[Tuner] Scenario B complete. Pass=%v\n", result.Pass)
	return result
}
