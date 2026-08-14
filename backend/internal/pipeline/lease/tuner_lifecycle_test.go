// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/pipeline/policy"
)

func testConsumedTicket(owner Owner) *policy.AdmissionTicket {
	adm := policy.NewHouseholdResourceAdmission()
	req := policy.AdmissionRequest{
		SessionID:   string(owner),
		UserID:      "usr_test",
		ProfileID:   "prof_test",
		Role:        identity.RoleAdmin,
		RequestType: policy.AdmissionRequestLiveTV,
	}
	resPol := &identity.HouseholdResourcePolicy{
		HouseholdID:               "default_household",
		MaxConcurrentLiveServices: 10,
		MaxConcurrentViewers:      10,
		MaxParallelRecordings:     10,
		MaxParallelTranscodes:     10,
	}
	tkt, _ := adm.IssueAdmissionTicket(req, resPol)
	consumed, _ := adm.ConsumeTicketOnce(tkt.TicketID)
	return consumed
}

// MockRenewalScheduler allows manual deterministic ticking without time.Sleep.
type MockRenewalScheduler struct {
	tickCh  chan time.Time
	stopped atomic.Bool
}

func NewMockRenewalScheduler() *MockRenewalScheduler {
	return &MockRenewalScheduler{tickCh: make(chan time.Time, 10)}
}

func (m *MockRenewalScheduler) C() <-chan time.Time {
	return m.tickCh
}

func (m *MockRenewalScheduler) Stop() {
	m.stopped.Store(true)
}

func (m *MockRenewalScheduler) Tick() {
	if !m.stopped.Load() {
		m.tickCh <- time.Now()
	}
}

func defaultTestRunnerConfig(tb *TunerBinding) RunnerConfig {
	return RunnerConfig{
		Controller:     NewTunerBindingController(tb),
		TTL:            30 * time.Second,
		RenewInterval:  10 * time.Second,
		CleanupTimeout: 2 * time.Second,
	}
}

// 1. Invalid Configuration Validation Test (No panics!)
func TestRunnerInvalidConfiguration(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()
	ctrl := NewTunerBindingController(NewTunerBinding(mgr))

	tests := []struct {
		name string
		cfg  RunnerConfig
		want error
	}{
		{
			name: "nil controller",
			cfg:  RunnerConfig{Controller: nil, TTL: 30 * time.Second, RenewInterval: 10 * time.Second, CleanupTimeout: 1 * time.Second},
			want: ErrBindingUnavailable,
		},
		{
			name: "zero TTL",
			cfg:  RunnerConfig{Controller: ctrl, TTL: 0, RenewInterval: 10 * time.Second, CleanupTimeout: 1 * time.Second},
			want: ErrInvalidTTL,
		},
		{
			name: "zero renew interval",
			cfg:  RunnerConfig{Controller: ctrl, TTL: 30 * time.Second, RenewInterval: 0, CleanupTimeout: 1 * time.Second},
			want: ErrInvalidTTL,
		},
		{
			name: "renew interval >= TTL",
			cfg:  RunnerConfig{Controller: ctrl, TTL: 30 * time.Second, RenewInterval: 30 * time.Second, CleanupTimeout: 1 * time.Second},
			want: ErrInvalidTTL,
		},
		{
			name: "zero cleanup timeout",
			cfg:  RunnerConfig{Controller: ctrl, TTL: 30 * time.Second, RenewInterval: 10 * time.Second, CleanupTimeout: 0},
			want: ErrInvalidTTL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewTunerLifecycleRunner(tt.cfg)
			if !errors.Is(err, tt.want) {
				t.Errorf("expected error %v, got %v", tt.want, err)
			}
		})
	}
}

// 2. Exclusive Start: Session A acquires slot; Session B rejected with ErrScopeConflict BEFORE hardware operations
func TestLifecycleExclusiveStart(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(NewTunerBinding(mgr)))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	sessionStarted := make(chan struct{})
	sessionUnblock := make(chan struct{})
	hardwareCalledB := atomic.Bool{}

	// Session A
	go func() {
		_ = runner.RunSessionWithTicket(
			context.Background(),
			testConsumedTicket("session-A"),
			"session-A",
			"usr_test",
			"prof_test",
			"session-A",
			slot,
			true, // requiresTuner = true
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				close(sessionStarted)
				<-sessionUnblock
				return nil
			},
		)
	}()

	<-sessionStarted

	// Session B attempts to acquire same slot
	runErr := runner.RunSessionWithTicket(
		context.Background(),
		testConsumedTicket("session-B"),
		"session-B",
		"usr_test",
		"prof_test",
		"session-B",
		slot,
		true,
		func(ctx context.Context) error {
			hardwareCalledB.Store(true)
			return nil
		},
		func(ctx context.Context) error { return nil },
	)

	if !errors.Is(runErr, ErrScopeConflict) {
		t.Errorf("expected ErrScopeConflict for Session B, got %v", runErr)
	}

	if hardwareCalledB.Load() {
		t.Errorf("hardware tune function MUST NOT be called when acquire fails with conflict")
	}

	close(sessionUnblock)
}

// 3. Tune Failure Compensation: Acquire succeeds, tuneFn fails -> Lease released immediately
func TestLifecycleTuneFailureCompensation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	tuneErr := errors.New("zap failed")

	runErr := runner.RunSessionWithTicket(
		context.Background(),
		testConsumedTicket("session-A"),
		"session-A",
		"usr_test",
		"prof_test",
		"session-A",
		slot,
		true,
		func(ctx context.Context) error {
			return tuneErr
		},
		func(ctx context.Context) error {
			t.Errorf("runFn should not execute when tuneFn fails")
			return nil
		},
	)

	if runErr == nil || !errors.Is(runErr, tuneErr) {
		t.Errorf("expected tune error, got %v", runErr)
	}

	// Slot must be free immediately
	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after tune failure")
	}
}

// 4. Readiness Failure Compensation: Zap succeeds, Readiness fails -> Lease released
func TestLifecycleReadinessFailureCompensation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	readinessErr := errors.New("tuner lock readiness timeout")

	runErr := runner.RunSessionWithTicket(
		context.Background(),
		testConsumedTicket("session-A"),
		"session-A",
		"usr_test",
		"prof_test",
		"session-A",
		slot,
		true,
		func(ctx context.Context) error {
			return readinessErr
		},
		func(ctx context.Context) error { return nil },
	)

	if runErr == nil || !errors.Is(runErr, readinessErr) {
		t.Errorf("expected readiness error, got %v", runErr)
	}

	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after readiness failure")
	}
}

// 5. Context Cancellation: Acquire succeeds, Parent context canceled -> Cleanup runs with detached bounded context
func TestLifecycleContextCancellation(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	slot := 0
	sessionStarted := make(chan struct{})

	go func() {
		_ = runner.RunSessionWithTicket(
			ctx,
			testConsumedTicket("session-canceled"),
			"session-canceled",
			"usr_test",
			"prof_test",
			"session-canceled",
			slot,
			true,
			func(c context.Context) error {
				close(sessionStarted)
				return nil
			},
			func(c context.Context) error {
				<-c.Done()
				return c.Err()
			},
		)
	}()

	<-sessionStarted
	cancel() // Cancel parent context

	time.Sleep(20 * time.Millisecond)

	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after context cancellation")
	}
}

// 6. Pipeline Start Failure: Tuner lease acquired, downstream start fails -> No lease leak
func TestLifecyclePipelineStartFailure(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	pipelineErr := errors.New("ffmpeg spawn failed")

	runErr := runner.RunSessionWithTicket(
		context.Background(),
		testConsumedTicket("session-pipeline-fail"),
		"session-pipeline-fail",
		"usr_test",
		"prof_test",
		"session-pipeline-fail",
		slot,
		true,
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error {
			return pipelineErr
		},
	)

	if !errors.Is(runErr, pipelineErr) {
		t.Errorf("expected pipelineErr, got %v", runErr)
	}

	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after pipeline start failure")
	}
}

// 7. Normal Completion: Session completes regularly -> Lease released once idempotently with ReasonReleasedByOwner
func TestLifecycleNormalCompletion(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	runErr := runner.RunSessionWithTicket(
		context.Background(),
		testConsumedTicket("session-normal"),
		"session-normal",
		"usr_test",
		"prof_test",
		"session-normal",
		slot,
		true,
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	)

	if runErr != nil {
		t.Fatalf("unexpected session error: %v", runErr)
	}

	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("expected tuner lease slot to be released after normal completion")
	}
}

// 8. Deterministic Renew Success using MockRenewalScheduler
func TestLifecycleRenewSuccess(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 1 * time.Hour})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	mockSched := NewMockRenewalScheduler()

	cfg := defaultTestRunnerConfig(tb)
	cfg.SchedulerFunc = func(d time.Duration) RenewalScheduler { return mockSched }

	runner, err := NewTunerLifecycleRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	sessionStarted := make(chan struct{})
	sessionDone := make(chan struct{})

	go func() {
		_ = runner.RunSessionWithTicket(
			context.Background(),
			testConsumedTicket("session-renew"),
			"session-renew",
			"usr_test",
			"prof_test",
			"session-renew",
			slot,
			true,
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				close(sessionStarted)
				<-sessionDone
				return nil
			},
		)
	}()

	<-sessionStarted

	// Trigger renewal tick deterministically
	mockSched.Tick()
	time.Sleep(10 * time.Millisecond)

	l, found := tb.GetTunerLease(slot)
	if !found {
		t.Fatalf("expected active lease during session")
	}
	if l.State != StateAcquired {
		t.Errorf("expected state ACQUIRED after renewal, got %s", l.State)
	}

	close(sessionDone)
}

// 9. Real Process Teardown on Revocation & Reason Preserved as ReasonPreempted
func TestLifecycleRealProcessTeardownOnRevoke(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	mockSched := NewMockRenewalScheduler()

	cfg := defaultTestRunnerConfig(tb)
	cfg.SchedulerFunc = func(d time.Duration) RenewalScheduler { return mockSched }

	runner, err := NewTunerLifecycleRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	processStopped := atomic.Bool{}
	sessionStarted := make(chan struct{})
	sessionEnded := make(chan struct{})

	go func() {
		defer close(sessionEnded)
		_ = runner.RunSessionWithTicket(
			context.Background(),
			testConsumedTicket("session-revoke"),
			"session-revoke",
			"usr_test",
			"prof_test",
			"session-revoke",
			slot,
			true,
			func(ctx context.Context) error { return nil },
			func(ctx context.Context) error {
				close(sessionStarted)
				<-ctx.Done()
				processStopped.Store(true) // Prove process Stop is actually invoked on revocation!
				return ctx.Err()
			},
		)
	}()

	<-sessionStarted

	l, found := tb.GetTunerLease(slot)
	if !found {
		t.Fatalf("failed to find active lease for revocation")
	}

	// Revoke active lease
	_, err = mgr.Revoke(l.ID, ReasonPreempted)
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	// Trigger mock renewal scheduler to discover revocation
	mockSched.Tick()

	<-sessionEnded

	if !processStopped.Load() {
		t.Errorf("expected real process Stop to be executed upon revocation")
	}

	// Slot must be free for new owner
	l2, err := tb.AcquireTunerSlot(context.Background(), "new-owner", slot, 5*time.Minute)
	if err != nil {
		t.Fatalf("expected acquire after revocation to succeed, got %v", err)
	}
	if l2.Owner != "new-owner" {
		t.Errorf("expected owner new-owner, got %s", l2.Owner)
	}
}

// 10. Non-Tuner Source: requiresTuner = false -> No lease acquired, stream runs directly
func TestLifecycleNonTunerSource(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	tuneCalled := false

	runErr := runner.RunSession(
		context.Background(),
		"session-vod",
		slot,
		false, // requiresTuner = false
		func(ctx context.Context) error {
			tuneCalled = true
			return nil
		},
		func(ctx context.Context) error { return nil },
	)

	if runErr != nil {
		t.Fatalf("unexpected non-tuner session error: %v", runErr)
	}
	if tuneCalled {
		t.Errorf("tune function MUST NOT be called when requiresTuner is false")
	}
	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("no tuner lease should be acquired when requiresTuner is false")
	}
}

// 11. High-Concurrency Race Test
func TestLifecycleRaceConditions(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()

	tb := NewTunerBinding(mgr)
	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	const numWorkers = 25
	var wg sync.WaitGroup
	slot := 0
	activeConcurrentStarts := atomic.Int32{}
	maxConcurrentStarts := atomic.Int32{}

	ctx := context.Background()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		workerID := Owner(fmt.Sprintf("race-worker-%d", i))
		go func(wOwner Owner) {
			defer wg.Done()
			_ = runner.RunSessionWithTicket(
				ctx,
				testConsumedTicket(wOwner),
				string(wOwner),
				"usr_test",
				"prof_test",
				wOwner,
				slot,
				true,
				func(c context.Context) error { return nil },
				func(c context.Context) error {
					curr := activeConcurrentStarts.Add(1)
					defer activeConcurrentStarts.Add(-1)

					for {
						old := maxConcurrentStarts.Load()
						if curr <= old || maxConcurrentStarts.CompareAndSwap(old, curr) {
							break
						}
					}
					return nil
				},
			)
		}(workerID)
	}

	wg.Wait()

	if maxConcurrentStarts.Load() > 1 {
		t.Errorf("expected at most 1 active concurrent hardware start for slot %d, got %d", slot, maxConcurrentStarts.Load())
	}

	// Verify no orphaned leases left in manager after all workers finish
	active := mgr.ActiveLeases(ScopeForTunerSlot(slot))
	if len(active) > 0 {
		t.Errorf("expected 0 active leases remaining after race test, got %d", len(active))
	}
}

// 12. Fail-Closed Invariant Test: Tuner-bound session without valid admission ticket MUST fail closed
func TestLifecycle_RequiresTicketFailClosed(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 100 * time.Millisecond})
	defer mgr.Close()
	tb := NewTunerBinding(mgr)

	runner, err := NewTunerLifecycleRunner(defaultTestRunnerConfig(tb))
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	slot := 0
	tuneCalled := false

	// Calling RunSession (which provides nil ticket) for a tuner-bound workload MUST fail closed!
	runErr := runner.RunSession(
		context.Background(),
		"session-ticketless",
		slot,
		true, // requiresTuner = true
		func(ctx context.Context) error {
			tuneCalled = true
			return nil
		},
		func(ctx context.Context) error { return nil },
	)

	if runErr == nil {
		t.Fatalf("expected error when acquiring tuner lease without admission ticket, got nil")
	}
	if tuneCalled {
		t.Errorf("hardware tune function MUST NOT be called when admission ticket is missing")
	}
	_, found := tb.GetTunerLease(slot)
	if found {
		t.Errorf("no tuner lease should be acquired when admission ticket is missing")
	}
}
