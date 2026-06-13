package intents

import (
	"context"
	"time"

	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/rs/zerolog"
)

// sessionDeleter is the optional store capability to remove a session record.
type sessionDeleter interface {
	DeleteSession(ctx context.Context, id string) error
}

// rollbackPersistedStart compensates a start whose event publish failed after the
// session and its idempotency mapping were already persisted. Without it the
// session lingers in NEW forever (nothing will ever start it) and its idempotency
// key replays the un-started session to every retry. The idempotency mapping is
// dropped FIRST so retries always proceed with a fresh session even if the session
// delete then fails; an orphaned NEW record holds no tuner/process resources (it
// never started). Uses a detached context because the publish failure may itself
// be a context cancellation.
func rollbackPersistedStart(ctx context.Context, store SessionStore, logger zerolog.Logger, sessionID, idempotencyKey string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if cleaner, ok := store.(sessionstore.IdempotencyCleaner); ok {
		if _, err := cleaner.DeleteIdempotencyIfMatch(cleanupCtx, idempotencyKey, sessionID); err != nil {
			logger.Error().Err(err).Str("idem_key", idempotencyKey).Msg("rollback: failed to delete idempotency mapping after publish failure")
		}
	}
	if deleter, ok := store.(sessionDeleter); ok {
		if err := deleter.DeleteSession(cleanupCtx, sessionID); err != nil {
			logger.Error().Err(err).Str("sid", sessionID).Msg("rollback: failed to delete session after publish failure")
		}
	}
}

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
