// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package intents

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/rs/zerolog"
)

// TestService_ProcessIntent_StartPublishFailureRollsBack ensures a start whose
// event publish fails does not leave a persisted NEW session and idempotency key
// behind. Otherwise the session never starts (the event never reached the
// orchestrator) and every retry is served the dead session as an idempotent
// replay.
func TestService_ProcessIntent_StartPublishFailureRollsBack(t *testing.T) {
	deps := newMockDeps()
	deps.bus.err = errors.New("bus unavailable")
	svc := NewService(deps)

	res, err := svc.ProcessIntent(context.Background(), Intent{
		Type:          model.IntentTypeStreamStart,
		SessionID:     "sid-1",
		ServiceRef:    "1:0:1:1337:42:99:0:0:0:0:",
		Params:        map[string]string{"profile": "high"},
		CorrelationID: "corr-1",
		Mode:          model.ModeLive,
		UserAgent:     "unit-test",
		Logger:        zerolog.Nop(),
	})

	if err == nil {
		t.Fatalf("expected publish error, got result %#v", res)
	}
	if err.Kind != ErrorPublishUnavailable {
		t.Fatalf("expected ErrorPublishUnavailable, got %v", err.Kind)
	}

	// The session was persisted, so it must be rolled back.
	if deps.store.putCalls != 1 {
		t.Fatalf("expected the session to be persisted once, got %d put calls", deps.store.putCalls)
	}
	idemKey := deps.store.putIdemKey
	if idemKey == "" {
		t.Fatal("expected an idempotency key to be computed")
	}

	if deps.store.deleteCalls == 0 || deps.store.deleteKey != idemKey {
		t.Errorf("expected idempotency mapping %q to be rolled back, got %d delete calls for key %q",
			idemKey, deps.store.deleteCalls, deps.store.deleteKey)
	}
	if deps.store.deleteSessionCalls == 0 || deps.store.deletedSessionID != "sid-1" {
		t.Errorf("expected session sid-1 to be deleted, got %d session deletes for %q",
			deps.store.deleteSessionCalls, deps.store.deletedSessionID)
	}
}
