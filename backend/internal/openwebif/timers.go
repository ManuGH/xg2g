package openwebif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GetTimers retrieves the list of timers from the receiver.
func (c *Client) GetTimers(ctx context.Context) ([]Timer, error) {
	// Timers change frequently, so we don't cache them aggressively
	// or we use a very short TTL if we did. For now, no caching.
	body, err := c.get(ctx, "/api/timerlist", "timers.list", nil)
	if err != nil {
		return nil, err
	}

	var payload TimerListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		c.loggerFor(ctx).Error().Err(err).Str("event", "openwebif.decode").Str("operation", "timers.list").Msg("failed to decode timer list")
		return nil, err
	}

	return payload.Timers, nil
}

// AddTimer schedules a new recording.
func (c *Client) AddTimer(ctx context.Context, sRef string, begin, end int64, name, description string) error {
	// URL Encode parameters
	params := url.Values{}
	params.Set("sRef", sRef)
	params.Set("begin", strconv.FormatInt(begin, 10))
	params.Set("end", strconv.FormatInt(end, 10))
	params.Set("name", name)
	params.Set("description", description)
	// Defaults
	params.Set("disabled", "0")
	params.Set("justplay", "0")
	params.Set("after_event", "3") // Auto

	path := "/api/timeradd?" + params.Encode()
	body, err := c.get(ctx, path, "timers.add", nil)
	if err != nil {
		return err
	}

	var resp TimerOpResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to decode timer add response: %w", err)
	}

	if !resp.Result {
		return timerOperationError("timers.add", http.StatusOK, resp.Message)
	}

	return nil
}

// DeleteTimer removes an existing timer.
// Enigma2 requires exact matching of sRef, begin, and end.
func (c *Client) DeleteTimer(ctx context.Context, sRef string, begin, end int64) error {
	params := url.Values{}
	params.Set("sRef", sRef)
	params.Set("begin", strconv.FormatInt(begin, 10))
	params.Set("end", strconv.FormatInt(end, 10))

	path := "/api/timerdelete?" + params.Encode()
	body, err := c.get(ctx, path, "timers.delete", nil)
	if err != nil {
		return err
	}

	var resp TimerOpResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to decode timer delete response: %w", err)
	}

	if !resp.Result {
		return timerOperationError("timers.delete", http.StatusOK, resp.Message)
	}

	return nil
}

// UpdateTimer updates a timer using the best available strategy.
// Strategy:
// 1. Detect capabilities (Cached).
// 2. If supported, try Flavor A (most common for changes).
// 3. If Flavor A rejected (specific error), Promote to Flavor B and retry once.
// 4. If unsupported or failed, use Fail-Closed Fallback (Add -> Delete).
func (c *Client) UpdateTimer(ctx context.Context, oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string, enabled bool) error {
	// 0. Detect
	cap, err := c.DetectTimerChange(ctx)
	if err != nil {
		observeTimerUpdate("terminal_failure", "none", TimerChangeFlavorUnknown, TimerChangeCap{})
		return err
	}

	result := "terminal_failure"
	reason := "none"
	nativeFlavor := TimerChangeFlavorUnknown

	defer func() {
		observeTimerUpdate(result, reason, nativeFlavor, cap)
	}()

	var fallbackReason string
	if !cap.Supported {
		fallbackReason = "unsupported"
	}

	// 1. Check Forbidden
	if cap.Forbidden {
		return ErrForbidden
	}

	// 2. Supported Path
	if cap.Supported {
		// Determine Flavor
		flavor := cap.Flavor
		if flavor == TimerChangeFlavorUnknown {
			flavor = TimerChangeFlavorA // Default start
		}
		nativeFlavor = flavor

		// Helper to execute update
		doUpdate := func(f TimerChangeFlavor) error {
			// 2b. Flavor B (Strict & In-Place Only)
			// User Requirement: "Flavor B is in-place property update only. Must use old identity."
			var params url.Values
			if f == TimerChangeFlavorB {
				// Identity Guard: If identity changes, we CANNOT use Flavor B (because it uses old identity to target).
				// We must fall back to Add+Delete.
				identityChanged := oldSRef != newSRef || oldBegin != newBegin || oldEnd != newEnd
				if identityChanged {
					fallbackReason = "identity_mismatch"
					// Return synthetic error to break out and hit fallback
					return fmt.Errorf("flavor B does not support identity changes")
				}
				params = c.buildTimerChangeFlavorB(oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, name, description, enabled)
			} else {
				// Flavor A (Channel + Change Params)
				params = c.buildTimerChangeFlavorA(oldSRef, oldBegin, oldEnd, newSRef, newBegin, newEnd, name, description)
			}

			path := "/api/timerchange?" + params.Encode()
			// Use a shorter timeout for the update attempt? Currently inheriting ctx.
			body, err := c.get(ctx, path, "timers.op.change", nil)
			if err != nil {
				// Technical error (Network/500). DO NOT Promote.
				// User Requirement: "Never promote on technical errors"
				return err
			}

			// Decode Response
			var resp TimerOpResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				return fmt.Errorf("failed to decode timer update response: %w", err)
			}
			if !resp.Result {
				// Logic Failure (200 OK but Result=false).
				// Convert to typed timer operation error for classification.
				return timerOperationError("timers.change", http.StatusOK, resp.Message)
			}
			return nil
		}

		// Attempt 1 (Native)
		err := doUpdate(flavor)
		if err == nil {
			// Success!
			result = "success"
			// If we started Unknown, update cache to confirmed flavor
			if cap.Flavor == TimerChangeFlavorUnknown {
				cap.Flavor = flavor // Confirm A or whatever we used
				cap.DetectedAt = time.Now()
				var copy = cap
				c.timerChangeCap.Store(&copy)
			}
			return nil
		}

		// --- ELIGIBILITY CHECK FOR PROMOTION OR FALLBACK ---

		// 2.1 Technical Error Check (Terminal)
		// User requirement 2: "Technical errors must be terminal (no fallback)"
		if c.isTechnicalError(err) {
			return err
		}

		// 2.2 Conflict Check (Terminal)
		// User requirement 3: "Conflicts are semantic and terminal (no fallback)"
		if c.isConflictError(err) {
			return err
		}

		// 2.3 Promotion Check (A -> B)
		// Only if we are at A and it's a param rejection
		if flavor == TimerChangeFlavorA {
			var owiErr *OWIError
			if errors.As(err, &owiErr) && c.shouldPromoteAToB(owiErr) {
				// Promote
				c.log.Warn().Err(err).Msg("UpdateTimer: promoting to Flavor B based on receiver feedback")
				errRetry := doUpdate(TimerChangeFlavorB)
				if errRetry == nil {
					// Success on B! Cache it.
					result = "success"
					cap.Flavor = TimerChangeFlavorB
					cap.DetectedAt = time.Now()
					var copy = cap
					c.timerChangeCap.Store(&copy)
					return nil
				}

				// Retry on B failed.
				// Re-evaluate eligibility for fallback from this new error.
				if c.isTechnicalError(errRetry) || c.isConflictError(errRetry) {
					return errRetry
				}
				err = errRetry // Continue to fallback if allowed
			}
		}

		// 2.4 Final Fallback Eligibility Check
		// User requirement 1.3: "Param-rejection fallback allowed (when appropriate)"
		var owiErr *OWIError
		if errors.As(err, &owiErr) {
			if !c.isSafeForFallback(owiErr) {
				// Not a known safe fallback condition
				return err
			}
			fallbackReason = "param_rejection"
		} else if fallbackReason == "" {
			// If it's not an OWIError and not identity mismatch (which sets reason),
			// and we reach here, it's likely an unhandled error type that is NOT technical.
			// But according to rule 1.3, fallback is ONLY allowed for specific OWIError matches.
			return err
		}

		c.loggerFor(ctx).Warn().
			Str("reason", fallbackReason).
			Str("native_flavor", string(flavor)).
			Bool("cap_supported", cap.Supported).
			Str("cap_flavor", string(cap.Flavor)).
			Err(err).
			Msg("UpdateTimer: native update skipped/failed, using selective fallback")

		reason = fallbackReason
	}

	// 3. Fallback (Add First, Then Delete)
	// Fail-Closed: If Add Preflight fails, abort.

	// Preflight Validation (Pure)
	if newBegin >= newEnd {
		return fmt.Errorf("invalid timer period: begin >= end")
	}
	if newSRef == "" {
		return fmt.Errorf("missing service reference")
	}

	// Step A: Add Timer using standard method
	if err := c.AddTimer(ctx, newSRef, newBegin, newEnd, name, description); err != nil {
		return fmt.Errorf("fallback add failed: %w", err)
	}

	// Step B: Add Succeeded -> Delete Old using standard method
	// If delete fails now, we have a DUPLICATE.
	if err := c.DeleteTimer(ctx, oldSRef, oldBegin, oldEnd); err != nil {
		result = "partial_failure"
		// CRITICAL: Partial Failure.
		c.log.Error().
			Str("old_sref", oldSRef).
			Int64("old_begin", oldBegin).
			Msg("UpdateTimer: Fallback partial failure! Added new timer but failed to delete old one. Duplicate risk.")

		return ErrTimerUpdatePartial
	}

	result = "fallback_success"
	return nil
}

// shouldPromoteAToB decides if we should switch from Flavor A to Flavor B
// based on the error response from the server.
func (c *Client) shouldPromoteAToB(owiErr *OWIError) bool {
	// Rule: Limit to 400, or 200 (logic error).
	if owiErr.Status != http.StatusBadRequest && owiErr.Status != http.StatusOK {
		return false
	}

	msg := strings.ToLower(owiErr.Body)

	// Whitelist check: indicates receiver didn't understand "channel" or "change_*" params.
	keys := []string{"channel", "change_"}
	signals := []string{"unknown parameter", "unknown argument"}

	for _, s := range signals {
		if strings.Contains(msg, s) {
			for _, k := range keys {
				if strings.Contains(msg, k) {
					return true
				}
			}
		}
	}
	return false
}

// isSafeForFallback determines if a logic error (OWIError) is "safe" to trigger
// the Add-then-Delete fallback. Safe means it implies the receiver did not apply the change.
func (c *Client) isSafeForFallback(owiErr *OWIError) bool {
	// Rule 1.3: Param-rejection logic error (Status 400 or 200/Result=false)
	if owiErr.Status != http.StatusBadRequest && owiErr.Status != http.StatusOK {
		return false
	}

	// Whitelist match (same as promotion signals)
	msg := strings.ToLower(owiErr.Body)
	signals := []string{"unknown parameter", "unknown argument"}
	keys := []string{"channel", "change_", "sref", "begin", "end", "disabled"}

	for _, s := range signals {
		if strings.Contains(msg, s) {
			for _, k := range keys {
				if strings.Contains(msg, k) {
					return true
				}
			}
		}
	}

	// Also allow if it's the synthetic "identity change" error
	if strings.Contains(msg, "flavor b does not support identity changes") {
		return true
	}

	return false
}

func (c *Client) isConflictError(err error) bool {
	return IsTimerConflict(err)
}

// buildTimerChangeFlavorA builds parameters for Flavor A (channel + change_*).
// Used by: some distributions like openATV (variant).
// Keys: sRef, begin, end, channel, change_begin, change_end, change_name, change_description, deleteOldOnSave
func (c *Client) buildTimerChangeFlavorA(oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string) url.Values {
	params := url.Values{}
	// Identity
	params.Set("sRef", oldSRef)
	params.Set("begin", strconv.FormatInt(oldBegin, 10))
	params.Set("end", strconv.FormatInt(oldEnd, 10))

	// Changes
	params.Set("channel", newSRef)
	params.Set("change_begin", strconv.FormatInt(newBegin, 10))
	params.Set("change_end", strconv.FormatInt(newEnd, 10))
	params.Set("change_name", name)
	params.Set("change_description", description)

	params.Set("deleteOldOnSave", "1")
	return params
}

// buildTimerChangeFlavorB constructs parameters for "modern" OpenWebIF (e.g. Dreambox/forks).
// Behavior:
//   - Uses "sRef", "begin", "end" to identify the timer (based on OLD identity).
//   - Uses "name", "description" for property updates.
//   - Maps "enabled" bool to "disabled" param (0=enabled, 1=disabled).
//   - Does NOT use "channel" or "change_*" keys.
//   - NOTE: This flavor supports in-place property updates ONLY. Identity changes (move/reschedule)
//     MUST use the fallback add/delete mechanism.
func (c *Client) buildTimerChangeFlavorB(oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, desc string, enabled bool) url.Values {
	v := url.Values{}

	// Identity: Uses OLD identity to target the timer
	v.Set("sRef", oldSRef)
	v.Set("begin", strconv.FormatInt(oldBegin, 10))
	v.Set("end", strconv.FormatInt(oldEnd, 10))

	// Properties
	v.Set("name", name)
	v.Set("description", desc)

	// Enabled State (Inverted Logic: disabled=0 is enabled)
	if enabled {
		v.Set("disabled", "0")
	} else {
		v.Set("disabled", "1")
	}

	// Strictly NO other parameters to avoid confusion
	return v
}

// HasTimerChange checks if the receiver supports /api/timerchange
// DetectTimerChange checks for /api/timerchange support using a dedicated probe.
// It caches results according to strict rules:
// - Supported (200/400) -> cache
// - Forbidden (401/403) -> cache (short TTL)
// - MethodNotAllowed (405) -> Supported (cache) (Endpoint exists but GET disallowed)
// - Missing (404) -> cache
// - Unknown (5xx/Network) -> DO NOT cache
func (c *Client) DetectTimerChange(ctx context.Context) (TimerChangeCap, error) {
	// 1. Check Cache
	if val := c.timerChangeCap.Load(); val != nil {
		cap := val.(*TimerChangeCap)
		if cap != nil && !cap.DetectedAt.IsZero() {
			ttl := 10 * time.Minute
			if cap.Forbidden {
				ttl = 1 * time.Minute
			}
			if time.Since(cap.DetectedAt) < ttl {
				return *cap, nil
			}
		}
	}

	// 2. Probe
	// We use a safe GET request. "timers.change.detect" label.
	_, err := c.get(ctx, "/api/timerchange?__probe=1", "timers.change.detect", nil)

	cap := TimerChangeCap{
		DetectedAt: time.Now(),
	}

	// Helper to store cache
	store := func(cap TimerChangeCap) {
		cap.DetectedAt = time.Now()
		// Store as pointer to handle cache invalidation semantics (nil check)
		var copy = cap
		c.timerChangeCap.Store(&copy)
	}

	if err == nil {
		// 200 OK (Exits).
		cap.Supported = true
		cap.Flavor = TimerChangeFlavorUnknown // Will be promoted later
		store(cap)
		return cap, nil
	}

	// 3. Status Error Handling
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		switch owiErr.Status {
		case http.StatusNotFound:
			// 404 -> Missing. Supported=false. Cache it.
			cap.Supported = false
			store(cap)
			return cap, nil

		case http.StatusUnauthorized, http.StatusForbidden:
			// 401/403 -> Exists but forbidden. Supported=true, Forbidden=true. Cache it.
			cap.Supported = true
			cap.Forbidden = true
			store(cap)
			return cap, nil

		case http.StatusMethodNotAllowed:
			// 405 -> Method Not Allowed (e.g. GET forbidden). Supported=true. Cache it.
			cap.Supported = true
			store(cap)
			return cap, nil

		case http.StatusBadRequest:
			// 400 -> Exists (bad params). Supported=true. Cache it.
			cap.Supported = true
			cap.Flavor = TimerChangeFlavorUnknown
			store(cap)
			return cap, nil
		}
	}

	// 4. Unknown/Network Error (5xx or transport error)
	// User requirement: "Unknown nie cachen."
	return TimerChangeCap{}, err
}
