// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

func TestRecoverySweep_RecoverStale(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recovery_test")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	s, _ := store.OpenStateStore("sqlite", filepath.Join(tmpDir, "sessions.sqlite"))
	defer func() {
		if closer, ok := s.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	orch := &Orchestrator{
		Store:    s,
		LeaseTTL: 0,
	}

	ctx := context.Background()

	// 1. Setup Stale Session (STARTING). A real Live session records its tuner slot in
	// transitionStarting, so it is a lease participant; recovery probes that slot's lease.
	session := &model.SessionRecord{
		SessionID:   "stale-1",
		ServiceRef:  "ref1",
		State:       model.SessionStarting,
		ContextData: map[string]string{model.CtxKeyTunerSlot: "0"},
	}
	_ = s.PutSession(ctx, session)

	// No active lease on the slot -> probe acquires -> recovery should proceed.

	// 2. Run Recovery
	if err := orch.recoverStaleLeases(ctx); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	// 3. Verify
	rec, _ := s.GetSession(ctx, "stale-1")
	if rec.State != model.SessionNew {
		t.Errorf("expected NEW, got %s", rec.State)
	}
	if rec.ContextData["recovered"] != "true" {
		t.Error("missing recovered flag")
	}
}

func TestRecoverySweep_IgnoreActive(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recovery_active")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	s, _ := store.OpenStateStore("sqlite", filepath.Join(tmpDir, "sessions.sqlite"))
	defer func() {
		if closer, ok := s.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	orch := &Orchestrator{
		Store:    s,
		LeaseTTL: 0,
	}
	ctx := context.Background()

	// 1. Setup Active Session — a Live participant that recorded tuner slot 0 and still
	// holds that slot's lease. Recovery must probe the slot key, find it held, and skip.
	session := &model.SessionRecord{
		SessionID:   "active-1",
		ServiceRef:  "ref1",
		State:       model.SessionStarting,
		ContextData: map[string]string{model.CtxKeyTunerSlot: "0"},
	}
	_ = s.PutSession(ctx, session)

	// Acquire a valid lease on the tuner slot the session claims (the key recovery probes).
	key := model.LeaseKeyTunerSlot(0)
	_, _, _ = s.TryAcquireLease(ctx, key, "current-owner", 60*time.Second)

	// 2. Run Recovery
	_ = orch.recoverStaleLeases(ctx)

	// 3. Verify
	rec, _ := s.GetSession(ctx, "active-1")
	if rec.State != model.SessionStarting {
		t.Errorf("active session was touched: %s", rec.State)
	}
}

func TestRecoverySweep_IgnoreTerminal(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recovery_term")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	s, _ := store.OpenStateStore("sqlite", filepath.Join(tmpDir, "sessions.sqlite"))
	defer func() {
		if closer, ok := s.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	ctx := context.Background()

	// TestRecoverySweep_IgnoreTerminal only checks truly terminal states now (FAILED, STOPPED)
	orch := &Orchestrator{
		Store:    s,
		LeaseTTL: 0,
	}
	_ = s.PutSession(ctx, &model.SessionRecord{SessionID: "s2", State: model.SessionFailed})
	_ = s.PutSession(ctx, &model.SessionRecord{SessionID: "s3", State: model.SessionStopped})

	_ = orch.recoverStaleLeases(ctx)

	r2, _ := s.GetSession(ctx, "s2")
	if r2.State != model.SessionFailed {
		t.Error("Failed changed")
	}
	r3, _ := s.GetSession(ctx, "s3")
	if r3.State != model.SessionStopped {
		t.Error("Stopped changed")
	}
}

func TestRecoverySweep_RecoverReady(t *testing.T) {
	// Fix 11-1: READY sessions without lease must be recovered to FAILED (Zombies)
	tmpDir, _ := os.MkdirTemp("", "recovery_ready")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	s, _ := store.OpenStateStore("sqlite", filepath.Join(tmpDir, "sessions.sqlite"))
	defer func() {
		if closer, ok := s.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	ctx := context.Background()

	orch := &Orchestrator{
		Store:    s,
		LeaseTTL: 0,
	}
	// Setup Zombie READY session: a Live participant (recorded tuner slot 0) whose lease
	// has expired/vanished. The probe acquires the free slot -> recover to FAILED.
	_ = s.PutSession(ctx, &model.SessionRecord{SessionID: "zombie", State: model.SessionReady, ContextData: map[string]string{model.CtxKeyTunerSlot: "0"}})
	// Cheat: Acquire/Wait to expire lease (implicit or explicit)
	// If no lease exists, TryAcquire works -> Recover works.
	// If lease exists but expired, TryAcquire works -> Recover works.
	// We just ensure no active lease blocks us.

	if err := orch.recoverStaleLeases(ctx); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	r, _ := s.GetSession(ctx, "zombie")
	if r.State != model.SessionFailed {
		t.Errorf("expected Zombie READY to become FAILED, got %s", r.State)
	}
	if r.ContextData["recovered"] != "true" {
		t.Error("missing recovered flag")
	}
}

// TestRecoverySweep_SkipsLeaselessSession is M4 Part 1's RED control. A session that never
// participated in the tuner-lease protocol (recording/VOD style: no tuner_slot recorded) must
// NOT be recovered by the lease-acquisition probe, even when shouldRecover flags it. Before
// the fix, recoveryLeaseKey fell back to LeaseKeyService — a key the session never held — so
// TryAcquireLease succeeded trivially and force-failed a live, leaseless session. With
// LeaseTTL=0, shouldRecover is unconditionally true, isolating the probe/skip decision from
// the freshness clock (Part 2). RED without the recoveryLeaseKey skip: the READY recording is
// force-failed to FAILED.
func TestRecoverySweep_SkipsLeaselessSession(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recovery_leaseless")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	s, _ := store.OpenStateStore("sqlite", filepath.Join(tmpDir, "sessions.sqlite"))
	defer func() {
		if closer, ok := s.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	orch := &Orchestrator{
		Store:    s,
		LeaseTTL: 0,
	}
	ctx := context.Background()

	// Active recording-style session: READY (an intermediate state), no tuner slot recorded,
	// so it holds no tuner lease. It must be left untouched by probe-recovery.
	_ = s.PutSession(ctx, &model.SessionRecord{
		SessionID:  "recording-1",
		ServiceRef: "rec-ref",
		State:      model.SessionReady,
	})

	if err := orch.recoverStaleLeases(ctx); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	rec, _ := s.GetSession(ctx, "recording-1")
	if rec.State != model.SessionReady {
		t.Errorf("leaseless session was wrongly recovered (force-failed): got %s, want READY", rec.State)
	}
	if rec.ContextData["recovered"] == "true" {
		t.Error("leaseless session was wrongly marked recovered")
	}
}
