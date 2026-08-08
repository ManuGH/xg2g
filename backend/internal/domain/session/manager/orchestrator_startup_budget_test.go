package manager

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// hq50Spec is the profile shape from session 1619cee0: a hardware AV1 profile
// running in the HQ50 runtime mode, which carries the longest per-attempt ceiling.
func hq50Spec() model.ProfileSpec {
	return model.ProfileSpec{
		Name:                 "av1_hw",
		TranscodeVideo:       true,
		HWAccel:              "vaapi",
		EffectiveRuntimeMode: ports.RuntimeModeHQ50,
	}
}

// fastGeometryOrchestrator has a segment geometry whose structural cost leaves
// room for two attempts inside the default budget: 3 x 2s of media is a 6s ready
// floor, so an attempt costs 18s and two of them fit in 45s.
func fastGeometryOrchestrator() *Orchestrator {
	return &Orchestrator{LiveReadySegments: 3, LiveSegmentSeconds: 2}
}

// standardGeometryOrchestrator has the shipped default geometry: 3 x 6s of media
// is an 18s ready floor, so one attempt costs 30s and two do NOT fit in 45s.
func standardGeometryOrchestrator() *Orchestrator {
	return &Orchestrator{LiveReadySegments: config.DefaultHLSReadySegments, LiveSegmentSeconds: config.DefaultHLSSegmentSeconds}
}

// TestPlaylistReadyTimeout_ClampsToRemainingStartupBudget pins the fix for
// per-attempt ceilings stacking past the player's deadline: whatever the profile
// would allow, an attempt may never outlive the session-wide budget.
func TestPlaylistReadyTimeout_ClampsToRemainingStartupBudget(t *testing.T) {
	orch := &Orchestrator{}
	spec := hq50Spec()

	// Unbounded: the profile ceiling applies unchanged.
	if got := orch.playlistReadyTimeout(spec, false, startupAttempt{}); got != defaultSafariHQ50PlaylistReadyTimeout {
		t.Fatalf("unbounded attempt should get the profile ceiling %v, got %v", defaultSafariHQ50PlaylistReadyTimeout, got)
	}

	// Bounded with less left than the profile wants: the budget wins.
	attempt := startupAttempt{Deadline: time.Now().Add(8 * time.Second)}
	got := orch.playlistReadyTimeout(spec, false, attempt)
	if got > 8*time.Second || got <= 7*time.Second {
		t.Fatalf("expected the wait to be clamped to the ~8s left in the budget, got %v", got)
	}
}

// TestPlaylistReadyTimeout_TotalBudgetFitsUnderPlayerDeadline is the constraint the
// whole budget exists for. The player gives up after SESSION_READY_TIMEOUT_MS (60s,
// useLiveSessionController.ts); if the orchestrator can still be working then, the
// user gets the player's generic timeout instead of the real failure reason.
func TestPlaylistReadyTimeout_TotalBudgetFitsUnderPlayerDeadline(t *testing.T) {
	const playerDeadline = 60 * time.Second // keep in sync with SESSION_READY_TIMEOUT_MS

	if defaultLiveStartupBudget >= playerDeadline {
		t.Fatalf("startup budget %v must stay below the player deadline %v", defaultLiveStartupBudget, playerDeadline)
	}

	// The old failure mode, stated as arithmetic: the per-attempt ceilings alone
	// add up past the player deadline, which is why they must be clamped.
	stacked := defaultSafariHQ50PlaylistReadyTimeout + defaultPlaylistReadyTimeout
	if stacked <= playerDeadline {
		t.Fatalf("precondition: unclamped ceilings (%v) should exceed the player deadline %v", stacked, playerDeadline)
	}
}

// TestPlaylistReadyTimeout_RecoveryAttemptIsExplicit pins the bug that made the
// recovery budget unreachable for the fallback that actually fires in production.
// The HQ50-to-HQ25 hardening keeps the profile NAME ("av1_hw"), so sniffing the
// name told the orchestrator no recovery was in progress and the attempt silently
// got the generic 60s ceiling instead of the 35s recovery one.
func TestPlaylistReadyTimeout_RecoveryAttemptIsExplicit(t *testing.T) {
	orch := &Orchestrator{}
	hardened := hq50Spec()
	hardened.ForceSafariHQ25 = true
	hardened.EffectiveRuntimeMode = ports.RuntimeModeHQ25
	hardened.EffectiveModeSource = ports.RuntimeModeSourceRuntimeHardening

	if isStartupRecoveryProfile(hardened.Name) {
		t.Fatal("precondition: the hardened profile keeps a name that is not a recovery profile name")
	}

	got := orch.playlistReadyTimeout(hardened, false, startupAttempt{Index: 1, Recovery: true})
	if got != defaultRecoveryPlaylistReadyTimeout {
		t.Fatalf("a recovery attempt must get the recovery ceiling %v, got %v", defaultRecoveryPlaylistReadyTimeout, got)
	}

	// Without the explicit flag the same profile falls back to the generic ceiling —
	// the behaviour that made the recovery budget dead code for this fallback.
	if got := orch.playlistReadyTimeout(hardened, false, startupAttempt{}); got != defaultPlaylistReadyTimeout {
		t.Fatalf("expected the generic ceiling %v without the recovery flag, got %v", defaultPlaylistReadyTimeout, got)
	}

	// A profile whose name IS a recovery profile still resolves without the flag,
	// so a client that asks for one directly keeps the recovery budget.
	if got := orch.playlistReadyTimeout(model.ProfileSpec{Name: profiles.ProfileRepair}, false, startupAttempt{}); got != defaultRecoveryPlaylistReadyTimeout {
		t.Fatalf("named recovery profile must keep the recovery ceiling, got %v", got)
	}
}

// TestPlaylistReadyTimeout_VODStaysUnbounded keeps recordings out of the live
// budget: they legitimately take longer and are not waited on by the live player.
func TestPlaylistReadyTimeout_VODStaysUnbounded(t *testing.T) {
	orch := &Orchestrator{}
	if got := orch.playlistReadyTimeout(model.ProfileSpec{Name: "default"}, true, startupAttempt{}); got != defaultVODPlaylistReadyTimeout {
		t.Fatalf("expected the VOD ceiling %v, got %v", defaultVODPlaylistReadyTimeout, got)
	}
}

// TestStartupAttempt_RemainingReportsBoundedness covers the zero-deadline contract
// the VOD path relies on.
func TestStartupAttempt_RemainingReportsBoundedness(t *testing.T) {
	if _, bounded := (startupAttempt{}).remaining(time.Now()); bounded {
		t.Fatal("a zero deadline must report as unbounded")
	}
	now := time.Now()
	left, bounded := startupAttempt{Deadline: now.Add(5 * time.Second)}.remaining(now)
	if !bounded || left != 5*time.Second {
		t.Fatalf("expected 5s bounded remaining, got %v bounded=%v", left, bounded)
	}
}

// TestStartupBudget_FirstAttemptLeavesRoomForTheRetry is the regression this whole
// budget model exists for.
//
// The clamp that keeps an attempt inside the budget used to clamp against the
// WHOLE remaining budget. So the first ready-wait always ended exactly at the
// deadline, the retry gate right after it always saw zero left, and every
// profile-hardening fallback (HQ50 -> HQ25 -> repair) was unreachable for the
// playlist timeout that is precisely what triggers it. Stated as arithmetic:
// after the first attempt has spent its whole allowance, a viable attempt must
// still fit.
func TestStartupBudget_FirstAttemptLeavesRoomForTheRetry(t *testing.T) {
	orch := fastGeometryOrchestrator()
	start := time.Now()
	budget := orch.newStartupBudget(start, false)

	if !budget.fitsRetry() {
		t.Fatalf("precondition: budget %v with a %v ready floor should hold two attempts", budget.Total, budget.ReadyFloor)
	}

	first := budget.attempt(0, false)
	usable, bounded := first.usable(start)
	if !bounded {
		t.Fatal("a live attempt must be bounded")
	}

	// The moment the first attempt has spent everything it is allowed to spend.
	exhausted := start.Add(usable)
	left, _ := first.remaining(exhausted)
	if left < budget.attemptCost() {
		t.Fatalf("after the first attempt spent its allowance, %v is left but a viable attempt needs %v — the retry gate would decline and the hardening ladder stays dead", left, budget.attemptCost())
	}
}

// TestStartupBudget_DeclinesRetryItCannotAfford is the other half of the contract.
// With the shipped segment geometry the budget genuinely holds only one attempt,
// and the honest response is to spend it all on the first one rather than
// squeezing two below the floor so both fail.
func TestStartupBudget_DeclinesRetryItCannotAfford(t *testing.T) {
	orch := standardGeometryOrchestrator()
	start := time.Now()
	budget := orch.newStartupBudget(start, false)

	if budget.ReadyFloor != 18*time.Second {
		t.Fatalf("expected an 18s ready floor for 3 x 6s segments, got %v", budget.ReadyFloor)
	}
	if budget.fitsRetry() {
		t.Fatalf("budget %v cannot hold two attempts of %v each", budget.Total, budget.attemptCost())
	}

	first := budget.attempt(0, false)
	if first.Reserve != 0 {
		t.Fatalf("a budget that cannot fund a retry must not reserve for one, got %v", first.Reserve)
	}
	usable, _ := first.usable(start)
	if usable != budget.Total {
		t.Fatalf("the only attempt must get the whole budget, got %v of %v", usable, budget.Total)
	}
}

// TestStartupBudget_LastAttemptReservesNothing keeps the reserve from stranding
// budget nobody can use.
func TestStartupBudget_LastAttemptReservesNothing(t *testing.T) {
	orch := fastGeometryOrchestrator()
	start := time.Now()
	budget := orch.newStartupBudget(start, false)

	last := budget.attempt(budget.RetryLimit, true)
	if last.Reserve != 0 {
		t.Fatalf("the last attempt has nothing to reserve for, got %v", last.Reserve)
	}
	usable, bounded := last.usable(start)
	if !bounded || usable != budget.Total {
		t.Fatalf("the last attempt may spend the whole remaining budget, got %v", usable)
	}
}

// TestStartupBudget_VODIsUnbounded keeps recordings out of the live arithmetic.
func TestStartupBudget_VODIsUnbounded(t *testing.T) {
	budget := standardGeometryOrchestrator().newStartupBudget(time.Now(), true)
	if budget.bounded() {
		t.Fatal("VOD startups must stay unbounded")
	}
	if budget.fitsRetry() {
		t.Fatal("an unbounded budget has no retry arithmetic to do")
	}
	if _, bounded := budget.attempt(0, false).usable(time.Now()); bounded {
		t.Fatal("an unbounded attempt must report unbounded")
	}
}

// TestStartupAttempt_PrepareDeadlineLeavesRoomForTheMedia pins the second hole the
// budget had: the pre-spawn work (tuning, preflight, plan probes) was outside the
// budget entirely, so a slow receiver could consume all of it before ffmpeg was
// started, and the session then died on a packager timeout for a packager that
// never got to run.
func TestStartupAttempt_PrepareDeadlineLeavesRoomForTheMedia(t *testing.T) {
	orch := fastGeometryOrchestrator()
	start := time.Now()
	budget := orch.newStartupBudget(start, false)
	first := budget.attempt(0, false)

	prepareDeadline, bounded := first.prepareDeadline(start)
	if !bounded {
		t.Fatal("a live attempt must bound its prepare phase")
	}

	usable, _ := first.usable(start)
	prepare := prepareDeadline.Sub(start)
	if prepare >= usable {
		t.Fatalf("prepare (%v) must not consume the attempt's whole allowance (%v)", prepare, usable)
	}
	if left := usable - prepare; left < budget.ReadyFloor {
		t.Fatalf("after prepare only %v is left, less than the %v of media the ready gate requires", left, budget.ReadyFloor)
	}
}

// TestStartupAttempt_PrepareDeadlineKeepsAFloor guards the degenerate direction:
// on a budget too tight to derive a meaningful prepare slice, the attempt still
// gets enough to reach the process. Otherwise a tight budget would turn every
// session into a prepare timeout instead of a diagnosis.
func TestStartupAttempt_PrepareDeadlineKeepsAFloor(t *testing.T) {
	now := time.Now()
	attempt := startupAttempt{
		Deadline:   now.Add(20 * time.Second),
		ReadyFloor: 18 * time.Second,
	}

	prepareDeadline, bounded := attempt.prepareDeadline(now)
	if !bounded {
		t.Fatal("a bounded attempt must bound its prepare phase")
	}
	if got := prepareDeadline.Sub(now); got != minPrepareBudget {
		t.Fatalf("expected the prepare floor %v, got %v", minPrepareBudget, got)
	}

	// And it never exceeds what the attempt itself may spend.
	tight := startupAttempt{Deadline: now.Add(3 * time.Second), ReadyFloor: 18 * time.Second}
	tightDeadline, _ := tight.prepareDeadline(now)
	if got := tightDeadline.Sub(now); got != 3*time.Second {
		t.Fatalf("prepare must not outlive the attempt: expected 3s, got %v", got)
	}
}

// TestStartupAttempt_UnboundedPrepareStaysUnbounded keeps VOD free of a bound the
// live path needs.
func TestStartupAttempt_UnboundedPrepareStaysUnbounded(t *testing.T) {
	if _, bounded := (startupAttempt{}).prepareDeadline(time.Now()); bounded {
		t.Fatal("an unbounded attempt must not bound its prepare phase")
	}
	if _, bounded := (startupAttempt{}).deadline(); bounded {
		t.Fatal("an unbounded attempt has no deadline to hand to adapters")
	}
}

// TestStartupAttempt_DeadlineExcludesTheReserve makes sure the deadline adapters
// bound their watchdogs with is this attempt's, not the whole budget's.
func TestStartupAttempt_DeadlineExcludesTheReserve(t *testing.T) {
	now := time.Now()
	attempt := startupAttempt{Deadline: now.Add(45 * time.Second), Reserve: 18 * time.Second}

	deadline, bounded := attempt.deadline()
	if !bounded {
		t.Fatal("a bounded attempt must expose its deadline")
	}
	if got := deadline.Sub(now); got != 27*time.Second {
		t.Fatalf("expected the attempt to end 27s in (45s budget less an 18s reserve), got %v", got)
	}
}

// TestPlaylistReadyTimeout_RespectsTheReserve is the clamp seen from the ready
// wait: it may take the attempt's share, never the reserve behind it.
func TestPlaylistReadyTimeout_RespectsTheReserve(t *testing.T) {
	orch := &Orchestrator{}
	now := time.Now()
	attempt := startupAttempt{Deadline: now.Add(40 * time.Second), Reserve: 18 * time.Second}

	// The default ceiling (60s) is longer than the 22s share, so the share wins.
	got := orch.playlistReadyTimeout(model.ProfileSpec{Name: "default"}, false, attempt)
	if got > 22*time.Second || got <= 21*time.Second {
		t.Fatalf("expected the wait clamped to the ~22s share, got %v", got)
	}
}

// TestLiveReadyFloor_UsesConfiguredGeometry pins where the floor comes from, and
// that an unconfigured orchestrator still gets the shipped default rather than a
// zero floor that would make every retry look affordable.
func TestLiveReadyFloor_UsesConfiguredGeometry(t *testing.T) {
	if got := (&Orchestrator{LiveReadySegments: 4, LiveSegmentSeconds: 3}).liveReadyFloor(); got != 12*time.Second {
		t.Fatalf("expected 4 x 3s = 12s, got %v", got)
	}

	defaulted := (&Orchestrator{}).liveReadyFloor()
	want := time.Duration(config.DefaultHLSReadySegments) * time.Duration(config.DefaultHLSSegmentSeconds) * time.Second
	if defaulted != want {
		t.Fatalf("an unconfigured orchestrator must fall back to the shipped geometry %v, got %v", want, defaulted)
	}
}

// TestStartupBudget_FastFailureStillRetries guards the retry path the reserve
// must NOT take away.
//
// The reserve shapes how long an attempt may WAIT; it does not decide whether a
// retry happens. A process that dies early — a tuner that had not settled, a
// relay that dropped the first connection — leaves most of the budget unspent,
// and that retry is worth taking even on a geometry whose budget could never
// have funded two full-length attempts.
func TestStartupBudget_FastFailureStillRetries(t *testing.T) {
	orch := standardGeometryOrchestrator()
	start := time.Now()
	budget := orch.newStartupBudget(start, false)

	if budget.fitsRetry() {
		t.Fatal("precondition: this geometry cannot fund two full-length attempts")
	}

	// The upstream drops 5s in, long before the attempt's allowance is spent.
	failedAt := start.Add(5 * time.Second)
	left, bounded := budget.attempt(0, false).remaining(failedAt)
	if !bounded {
		t.Fatal("a live attempt must be bounded")
	}
	if left < budget.attemptCost() {
		t.Fatalf("a failure at 5s leaves %v, which must still fund a %v attempt", left, budget.attemptCost())
	}
}
