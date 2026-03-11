package intents

import (
	"context"

	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
)

type startReplayResolution struct {
	correlationID string
}

func resolveStartReplay(ctx context.Context, store SessionStore, idempotencyKey, existingID, fallbackCorrelation string) (*startReplayResolution, bool, error) {
	replayCorrelation := fallbackCorrelation

	existingSession, err := store.GetSession(ctx, existingID)
	if err != nil {
		return nil, false, err
	}
	if existingSession != nil {
		if existingSession.CorrelationID != "" {
			replayCorrelation = existingSession.CorrelationID
		} else if existingSession.ContextData != nil {
			if cid := existingSession.ContextData["correlationId"]; cid != "" {
				replayCorrelation = cid
			}
		}
		if !existingSession.State.IsTerminal() {
			return &startReplayResolution{correlationID: replayCorrelation}, false, nil
		}
	}

	cleaner, ok := store.(sessionstore.IdempotencyCleaner)
	if !ok {
		// Stores without cleanup support treat the idempotency mapping as authoritative.
		// This preserves non-blocking replay semantics even if session visibility lags.
		return &startReplayResolution{correlationID: replayCorrelation}, false, nil
	}
	if _, err := cleaner.DeleteIdempotencyIfMatch(ctx, idempotencyKey, existingID); err != nil {
		return nil, false, err
	}
	return nil, true, nil
}
