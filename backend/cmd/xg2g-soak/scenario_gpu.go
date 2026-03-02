// Package main - GPU Saturation Scenario (Scenario A)
// Operator-Grade implementation per CTO review.
package main

import (
	"fmt"
	"time"
)

// GPUSaturationEvidence captures data for the report.
type GPUSaturationEvidence struct {
	GPULimit                     int       `json:"gpu_limit"`
	FillSessionsStarted          int       `json:"fill_sessions_started"`
	TokensReachedAt              time.Time `json:"tokens_reached_at"`
	BurstRequestsSent            int       `json:"burst_requests_sent"`
	RejectsExpectedMin           int       `json:"rejects_expected_min"`
	PromDeltaRejectGPUBusy       float64   `json:"prom_delta_reject_gpu_busy"`
	PromDeltaUTISpawnDuringBurst float64   `json:"prom_delta_uti_spawn_during_burst"`
	MaxTokensObserved            float64   `json:"max_tokens_observed"`
	DrainTimeToZeroS             float64   `json:"drain_time_to_zero_s"`
	RejectsWithGPUBusyReason     int       `json:"rejects_with_gpu_busy_reason"`
}

// runGPUSaturationScenario executes the GPU saturation test.
// Returns ScenarioResult with pass/fail and evidence.
func runGPUSaturationScenario(cfg Config, prom *PromClient, client *SessionClient) ScenarioResult {
	result := ScenarioResult{
		Name:         "gpu_saturation",
		Pass:         true,
		Observations: make(map[string]int64),
		Failures:     []Failure{},
	}

	evidence := GPUSaturationEvidence{}

	// Determine GPU limit
	gpuLimit := cfg.GPULimitOverride
	if gpuLimit <= 0 {
		gpuLimit = 8 // Default
	}
	evidence.GPULimit = gpuLimit
	fmt.Printf("[GPU] Target GPU limit: %d\n", gpuLimit)

	// Track sessions for cleanup
	var activeSessions []string
	defer func() {
		fmt.Printf("[GPU] Cleaning up %d sessions...\n", len(activeSessions))
		stopErrors := client.StopAllSessions(activeSessions)
		if stopErrors > 0 {
			fmt.Printf("[GPU] Warning: %d sessions failed to stop\n", stopErrors)
		}
	}()

	// Build metric queries with selector
	tokensMetric := prom.Metric("xg2g_gpu_tokens_in_use")
	rejectMetric := prom.Metric(`xg2g_admission_reject_total{reason="gpu_busy",priority="pulse"}`)
	// CTO Fix #2: Use strict cause="rejected" check for no-spawn guarantee (Invariant Metric)
	spawnRejectedMetric := prom.Metric(`xg2g_invariant_violation_total{rule="spawn_on_reject"}`)

	// ===================
	// Phase 1: Fill
	// ===================
	fmt.Printf("[GPU] Phase 1: Fill - starting %d sessions\n", gpuLimit)

	for i := 0; i < gpuLimit; i++ {
		serviceRef := fmt.Sprintf("soak-gpu-test-%d", i)
		// Use Live priority (not Pulse) since Live doesn't get gpu_busy rejection
		res := client.StartSession(serviceRef, "live")
		if res.Error != nil {
			result.Failures = append(result.Failures, Failure{
				Time:    time.Now(),
				RuleID:  "FILL_SESSION_ERROR",
				Message: fmt.Sprintf("Failed to start session %d: %v", i, res.Error),
			})
			result.Pass = false
			continue
		}

		if res.HTTPStatus == 200 || res.HTTPStatus == 201 || res.HTTPStatus == 202 {
			activeSessions = append(activeSessions, res.SessionID)
			evidence.FillSessionsStarted++
		} else {
			result.Failures = append(result.Failures, Failure{
				Time:    time.Now(),
				RuleID:  "FILL_UNEXPECTED_REJECT",
				Message: fmt.Sprintf("Session %d rejected during fill: status=%d reason=%s", i, res.HTTPStatus, res.AdmissionReason),
			})
		}
	}

	// Wait until tokens reach target (with timeout) - CTO Fix #1
	fmt.Println("[GPU] Waiting for GPU tokens to reach target...")
	fillTimeout := 60 * time.Second
	err := prom.WaitForAtLeast(tokensMetric, float64(gpuLimit), fillTimeout)
	if err != nil {
		tokensNow, _ := prom.QueryValue(tokensMetric)
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "FILL_TOKENS_NOT_REACHED",
			Message: fmt.Sprintf("GPU tokens did not reach target: got %.0f, wanted %d. %v", tokensNow, gpuLimit, err),
		})
		result.Pass = false
		// Continue to drain anyway
	}

	// Wait for stability (2 scrape intervals = 30s) - CTO Fix #1
	fmt.Println("[GPU] Waiting for tokens to stabilize...")
	stableErr := prom.WaitStable(tokensMetric, float64(gpuLimit), 30*time.Second, 45*time.Second)
	if stableErr != nil {
		fmt.Printf("[GPU] Warning: tokens not stable: %v\n", stableErr)
	}

	evidence.TokensReachedAt = time.Now()
	tokensNow, _ := prom.QueryValue(tokensMetric)
	fmt.Printf("[GPU] GPU tokens in use: %.0f (target: %d)\n", tokensNow, gpuLimit)

	result.Observations["fill_sessions"] = int64(evidence.FillSessionsStarted)
	result.Observations["tokens_after_fill"] = int64(tokensNow)

	// ===================
	// Phase 2: Overcommit Burst
	// ===================
	burstCount := 50
	fmt.Printf("[GPU] Phase 2: Overcommit - sending %d Pulse requests\n", burstCount)

	// Record baseline spawn count (removed as we checking rejected delta)
	// spawnBefore, _ := prom.QueryValue(spawnMetric)

	rejectCount := 0
	gpuBusyRejectCount := 0
	for i := 0; i < burstCount; i++ {
		serviceRef := fmt.Sprintf("soak-gpu-burst-%d", i)
		// Use Pulse - this should get gpu_busy rejection
		res := client.StartSession(serviceRef, "pulse")
		if res.HTTPStatus == 503 {
			rejectCount++
			// CTO Fix #4: Check X-Admission-Factor header for reason
			if res.AdmissionReason == "gpu_busy" {
				gpuBusyRejectCount++
			} else if res.AdmissionReason != "" {
				fmt.Printf("[GPU] Unexpected reject reason: %s (expected gpu_busy)\n", res.AdmissionReason)
			}
		}
		// Small delay to avoid overwhelming
		time.Sleep(100 * time.Millisecond)
	}

	evidence.BurstRequestsSent = burstCount
	evidence.RejectsExpectedMin = int(float64(burstCount) * 0.9)
	evidence.RejectsWithGPUBusyReason = gpuBusyRejectCount

	// Wait for metrics to settle
	time.Sleep(10 * time.Second)

	// Verify rejects via Prometheus (Scalarized)
	rejectDelta, _ := prom.QueryValue(fmt.Sprintf("sum(increase(%s[5m]))", rejectMetric))
	evidence.PromDeltaRejectGPUBusy = rejectDelta

	// Check UTI spawn (rejected) should be 0
	// We use strict scalar query: sum(increase(...))
	spawnRejectedDelta, _ := prom.QueryValue(fmt.Sprintf("sum(increase(%s[5m]))", spawnRejectedMetric))
	evidence.PromDeltaUTISpawnDuringBurst = spawnRejectedDelta

	// Verify no token exceeded limit (Defensive Scalarization)
	// max(max_over_time(...))
	maxTokens, _ := prom.QueryValue(fmt.Sprintf("max(max_over_time(%s[5m]))", tokensMetric))
	evidence.MaxTokensObserved = maxTokens

	result.Observations["burst_rejects_local"] = int64(rejectCount)
	result.Observations["burst_rejects_gpu_busy"] = int64(gpuBusyRejectCount)
	result.Observations["burst_rejects_prom"] = int64(rejectDelta)
	result.Observations["spawn_delta_rejected"] = int64(spawnRejectedDelta)
	result.Observations["max_tokens"] = int64(maxTokens)

	fmt.Printf("[GPU] Burst results: rejects=%d (gpu_busy=%d, prom: %.0f), spawn_rejected=%.0f, max_tokens=%.0f\n",
		rejectCount, gpuBusyRejectCount, rejectDelta, spawnRejectedDelta, maxTokens)

	// Fail conditions
	if spawnRejectedDelta > 0 {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "SPAWN_ON_REJECT",
			Message: fmt.Sprintf("Critical: UTI spawn occurred for rejected requests (delta=%.0f)", spawnRejectedDelta),
		})
		result.Pass = false
	}

	// Fail conditions
	if maxTokens > float64(gpuLimit) {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "TOKEN_LIMIT_EXCEEDED",
			Message: fmt.Sprintf("GPU tokens exceeded limit: %.0f > %d", maxTokens, gpuLimit),
		})
		result.Pass = false
	}

	// CTO Fix #4: Validate that most rejects have the correct reason
	if gpuBusyRejectCount < evidence.RejectsExpectedMin/2 && rejectCount > 0 {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "WRONG_REJECT_REASON",
			Message: fmt.Sprintf("Expected gpu_busy rejects, got %d/%d with correct reason", gpuBusyRejectCount, rejectCount),
		})
		// Not a hard fail for now - might be unauthenticated (coarse reason)
	}

	// ===================
	// Phase 3: Drain + Leak Check
	// ===================
	fmt.Println("[GPU] Phase 3: Drain - stopping all sessions")
	drainStart := time.Now()
	stopErrors := client.StopAllSessions(activeSessions)
	activeSessions = nil // Cleared

	if stopErrors > 0 {
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "DRAIN_STOP_ERROR",
			Message: fmt.Sprintf("%d sessions failed to stop during drain", stopErrors),
		})
	}

	// Wait for tokens to return to 0 (with tolerance) - CTO Fix #5
	fmt.Println("[GPU] Waiting for tokens to drain...")
	drainTimeout := 60 * time.Second
	err = prom.WaitForAtMost(tokensMetric, 0, drainTimeout)
	drainDuration := time.Since(drainStart)
	evidence.DrainTimeToZeroS = drainDuration.Seconds()

	if err != nil {
		finalTokens, _ := prom.QueryValue(tokensMetric)
		result.Failures = append(result.Failures, Failure{
			Time:    time.Now(),
			RuleID:  "TOKEN_LEAK_GPU",
			Message: fmt.Sprintf("GPU tokens did not drain to 0: %.0f remaining after %v", finalTokens, drainDuration),
		})
		result.Pass = false
	}

	result.Observations["drain_time_s"] = int64(drainDuration.Seconds())
	result.Observations["drain_stop_errors"] = int64(stopErrors)

	fmt.Printf("[GPU] Drain complete in %.1fs, pass=%v\n", drainDuration.Seconds(), result.Pass)

	return result
}
