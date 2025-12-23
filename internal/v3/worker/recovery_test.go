// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
)

func TestRecoverySweep_RecoverStale(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recovery_test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.OpenBoltStore(tmpDir) // Use real bolt store to test lease logic
	defer s.Close()

	orch := &Orchestrator{
		Store:    s,
		LeaseTTL: 100 * time.Millisecond,
	}

	ctx := context.Background()

	// 1. Setup Stale Session (STARTING, Expired Lease)
	session := &model.SessionRecord{
		SessionID:  "stale-1",
		ServiceRef: "ref1",
		State:      model.SessionStarting,
	}
	s.PutSession(ctx, session)

	// Create expired lease
	// We cheat by acquiring with short TTL and sleeping
	s.TryAcquireLease(ctx, "ref1", "old-owner", 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)

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
	defer os.RemoveAll(tmpDir)
	s, _ := store.OpenBoltStore(tmpDir)
	defer s.Close()

	orch := &Orchestrator{
		Store:    s,
		LeaseTTL: 100 * time.Millisecond,
	}
	ctx := context.Background()

	// 1. Setup Active Session
	session := &model.SessionRecord{
		SessionID:  "active-1",
		ServiceRef: "ref1",
		State:      model.SessionStarting,
	}
	s.PutSession(ctx, session)

	// Acquire valid lease
	// Phase 8-2b: Must matches the fallback key used by recovery (namespaced)
	key := LeaseKeyService("ref1")
	s.TryAcquireLease(ctx, key, "current-owner", 1*time.Second)

	// 2. Run Recovery
	orch.recoverStaleLeases(ctx)

	// 3. Verify
	rec, _ := s.GetSession(ctx, "active-1")
	if rec.State != model.SessionStarting {
		t.Errorf("active session was touched: %s", rec.State)
	}
}

func TestRecoverySweep_IgnoreTerminal(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recovery_term")
	defer os.RemoveAll(tmpDir)
	s, _ := store.OpenBoltStore(tmpDir)
	defer s.Close()

	orch := &Orchestrator{Store: s}
	ctx := context.Background()

	s.PutSession(ctx, &model.SessionRecord{SessionID: "s1", State: model.SessionReady})
	s.PutSession(ctx, &model.SessionRecord{SessionID: "s2", State: model.SessionFailed})

	orch.recoverStaleLeases(ctx)

	r1, _ := s.GetSession(ctx, "s1")
	if r1.State != model.SessionReady {
		t.Error("Ready changed")
	}
}
