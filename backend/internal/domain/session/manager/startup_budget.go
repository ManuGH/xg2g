// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"time"

	"github.com/ManuGH/xg2g/internal/config"
)

// How one live start is allowed to spend its budget.
//
// A live start has ONE budget for the whole thing (defaultLiveStartupBudget),
// deliberately below the player's own deadline so a failed start reports the
// reason it actually failed with instead of the player's generic timeout.
// This file decides how that budget is divided: between the phases of a single
// attempt, and between attempts.
//
// The rule everything here follows is that no phase may spend budget a later
// phase structurally needs. The budget used to be a bare deadline, and whichever
// phase reached it first took all of it. Two things followed from that:
//
//   - The first ready-wait was clamped to the entire remaining budget, so it
//     always ended exactly AT the deadline. The retry check right after it then
//     saw nothing left and declined — which made the profile-hardening ladder
//     (HQ50 -> HQ25 -> repair) unreachable for the playlist timeout that is
//     precisely what triggers it in production. The ladder existed, was tested,
//     and could not fire.
//   - The pre-spawn work — tuning, URL resolution, preflight, plan probes — sat
//     outside the budget entirely, because the deadline was only consulted
//     between attempts. A slow receiver could consume the whole budget before
//     ffmpeg was started at all, and the session then died on a packager
//     timeout that had nothing to do with the packager.
//
// Both are budget arithmetic, not policy, so they are fixed here rather than by
// moving the individual timeouts around again.

const (
	// attemptOverhead is what an attempt costs on top of the media it has to
	// produce: tuning, URL resolution, preflight, the plan probes, the process
	// spawn, and the ready poller's own granularity.
	//
	// It is deliberately one number instead of the sum of the per-step ceilings.
	// Those ceilings are worst cases that do not co-occur — a tune that needs
	// its full 10s is not also a preflight exhausting every retry — and adding
	// them up produces a per-attempt "cost" larger than the entire budget, which
	// would decline every retry unconditionally and reintroduce the dead ladder
	// this file exists to fix.
	attemptOverhead = 12 * time.Second

	// minPrepareBudget floors the prepare slice. A source needing longer than
	// this to tune and preflight has a problem no budget arithmetic can fix; the
	// floor is here so the arithmetic can never hand out a slice too small to
	// reach ffmpeg at all, which would turn a tight budget into a prepare
	// timeout for every session instead of a diagnosis.
	minPrepareBudget = 8 * time.Second
)

// startupBudget is the whole-start budget together with the facts needed to
// divide it. The zero value is unbounded, which is what VOD uses.
type startupBudget struct {
	// Deadline ends the budget. Zero means unbounded.
	Deadline time.Time
	// Total is the duration the deadline was built from. Kept separately because
	// the fit decision below is about the budget's size, not about how much of
	// it happens to be left at the moment it is asked.
	Total time.Duration
	// ReadyFloor is the media an attempt must produce before the ready gate can
	// possibly pass: the required segment count times the segment duration.
	ReadyFloor time.Duration
	// RetryLimit is the highest attempt index the startup loop may reach.
	RetryLimit int
}

// bounded reports whether the budget constrains anything at all.
func (b startupBudget) bounded() bool { return !b.Deadline.IsZero() }

// attemptCost is the least an attempt needs to have any chance of reaching
// ready: the media the ready gate demands, plus the fixed cost of getting a
// process to the point of producing it.
func (b startupBudget) attemptCost() time.Duration {
	return b.ReadyFloor + attemptOverhead
}

// fitsRetry reports whether the budget is large enough for two viable attempts.
//
// When it is not, the reserve is zero and attempt 0 gets everything. That is the
// honest outcome: squeezing two attempts into a budget that fits one leaves both
// below the floor, so both fail, and the session reports the truncated retry
// instead of the original diagnosis. Callers log the decision rather than
// silently running one attempt where the configuration implies two.
func (b startupBudget) fitsRetry() bool {
	if !b.bounded() || b.RetryLimit < 1 {
		return false
	}
	return b.Total >= 2*b.attemptCost()
}

// attempt derives the slice of the budget one attempt of the startup loop is
// allowed to spend.
func (b startupBudget) attempt(index int, recovery bool) startupAttempt {
	reserve := time.Duration(0)
	if index < b.RetryLimit && b.fitsRetry() {
		reserve = b.attemptCost()
	}
	return startupAttempt{
		Index:      index,
		Recovery:   recovery,
		Deadline:   b.Deadline,
		Reserve:    reserve,
		ReadyFloor: b.ReadyFloor,
	}
}

// startupAttempt carries the facts about one attempt inside the startup loop
// that decide how long each of its phases may take.
type startupAttempt struct {
	// Index is 0 for the first try and counts up after each restart.
	Index int
	// Recovery marks an attempt that follows a profile fallback. It is passed
	// explicitly because a hardened profile keeps its original name — the HQ50
	// to HQ25 fallback leaves "av1_hw" in place — so the name cannot be used to
	// tell that a recovery is in progress.
	Recovery bool
	// Deadline ends the whole startup budget. Zero means unbounded (VOD).
	Deadline time.Time
	// Reserve is budget this attempt may not touch because a later attempt needs
	// it. Zero on the last attempt, and zero whenever the budget cannot hold a
	// second viable attempt anyway.
	Reserve time.Duration
	// ReadyFloor is the media this attempt must produce before it can be ready.
	ReadyFloor time.Duration
}

// remaining reports the time left in the whole startup budget, and whether it is
// bounded. It ignores the reserve: this is the budget, not this attempt's share
// of it, and the retry decision needs the former.
func (a startupAttempt) remaining(now time.Time) (time.Duration, bool) {
	if a.Deadline.IsZero() {
		return 0, false
	}
	return a.Deadline.Sub(now), true
}

// usable reports what THIS attempt may still spend: the budget left, less
// whatever is held back for a later attempt.
func (a startupAttempt) usable(now time.Time) (time.Duration, bool) {
	remaining, bounded := a.remaining(now)
	if !bounded {
		return 0, false
	}
	return remaining - a.Reserve, true
}

// deadline is the moment this attempt stops being waited on. Adapters use it to
// bound their own startup watchdogs against the same clock the orchestrator uses.
func (a startupAttempt) deadline() (time.Time, bool) {
	if a.Deadline.IsZero() {
		return time.Time{}, false
	}
	return a.Deadline.Add(-a.Reserve), true
}

// prepareDeadline bounds the work that happens before the media process is
// spawned, so it cannot consume the encode time that follows it.
//
// The bound is derived, not apportioned: what prepare may take is what is left
// after the media the ready gate demands has been set aside. A prepare phase
// that overruns it fails with its own reason — a tune or preflight failure —
// which is the truthful diagnosis, instead of being reported later as a
// packager that never produced a playlist it was never given time to produce.
func (a startupAttempt) prepareDeadline(now time.Time) (time.Time, bool) {
	usable, bounded := a.usable(now)
	if !bounded {
		return time.Time{}, false
	}
	prepare := usable - a.ReadyFloor
	if prepare < minPrepareBudget {
		prepare = minPrepareBudget
	}
	if prepare > usable {
		prepare = usable
	}
	return now.Add(prepare), true
}

// newStartupBudget builds the budget for one start. VOD is unbounded: recordings
// legitimately take longer and no live player is waiting on them.
func (o *Orchestrator) newStartupBudget(startTime time.Time, vodMode bool) startupBudget {
	if vodMode {
		return startupBudget{RetryLimit: defaultStartupProcessRetryLimit}
	}
	total := defaultIfZero(o.LiveStartupBudget, defaultLiveStartupBudget)
	return startupBudget{
		Deadline:   startTime.Add(total),
		Total:      total,
		ReadyFloor: o.liveReadyFloor(),
		RetryLimit: defaultStartupProcessRetryLimit,
	}
}

// liveReadyFloor is the media an attempt must produce before the ready gate can
// pass: the required segment count times the segment duration.
//
// It is built from the CONFIGURED segment duration, which is an upper bound on
// the real one — the LL-HLS and short-startup-segment paths only ever shorten it
// (see planLiveSegmentLayout). A floor built from an upper bound errs toward
// reserving slightly too much for a retry, never too little, which is the safe
// direction: the cost of over-reserving is a shorter first attempt, the cost of
// under-reserving is the dead ladder this file exists to fix.
func (o *Orchestrator) liveReadyFloor() time.Duration {
	return time.Duration(o.liveReadySegments()) * o.liveSegmentDuration()
}

func (o *Orchestrator) liveSegmentDuration() time.Duration {
	seconds := o.LiveSegmentSeconds
	if seconds <= 0 {
		seconds = config.DefaultHLSSegmentSeconds
	}
	return time.Duration(seconds) * time.Second
}
