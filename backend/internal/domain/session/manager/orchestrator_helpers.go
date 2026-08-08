package manager

import (
	"context"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/receiverusage"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	pipelineLease "github.com/ManuGH/xg2g/internal/pipeline/lease"
)

type sessionContext struct {
	Mode       string
	ServiceRef string
	IsVOD      bool
}

type terminationCause struct {
	IsClean          bool
	ContextCancelled bool
	Error            error
}

type finalOutcome struct {
	State       model.SessionState
	Reason      model.ReasonCode
	DetailDebug string
}

const (
	defaultPlaylistReadyTimeout           = 60 * time.Second
	defaultSafariPlaylistReadyTimeout     = 30 * time.Second
	defaultSafariCPUPlaylistReadyTimeout  = 45 * time.Second
	defaultSafariHQ50PlaylistReadyTimeout = 75 * time.Second
	defaultRecoveryPlaylistReadyTimeout   = 35 * time.Second
	defaultVODPlaylistReadyTimeout        = 2 * time.Minute
	defaultStartupProcessRetryLimit       = 1

	// defaultLiveStartupBudget bounds the WHOLE live startup — every internal
	// attempt together — rather than each attempt on its own.
	//
	// The timeouts above are per-attempt ceilings, and they used to stack: an HQ50
	// profile could spend 75s on its first try and another 60s after a fallback,
	// so the orchestrator would still be working 135s in. The player gives up long
	// before that (SESSION_READY_TIMEOUT_MS, 60s, in useLiveSessionController.ts),
	// so everything past its deadline was unreachable budget that only decided
	// which message the user got: the player's generic "not ready in time" instead
	// of the reason the session actually failed with. Session 1619cee0 lost that
	// race by 80ms.
	//
	// So the budget sits under the player's deadline with margin for the terminal
	// transition to reach the client. Headroom is ample: over a week of staging
	// sessions the only startup that reached playlist-ready did so in 17.9s.
	// Keep this below the player's constant if either side ever moves.
	//
	// How the budget is divided between an attempt's phases, and between
	// attempts, lives in startup_budget.go.
	defaultLiveStartupBudget = 45 * time.Second
)

func (o *Orchestrator) resolveSession(ctx context.Context, e model.StartSessionEvent) (string, *model.SessionRecord, context.Context, error) {
	correlationID := e.CorrelationID
	var session *model.SessionRecord
	if o.Store != nil {
		if sess, err := o.Store.GetSession(ctx, e.SessionID); err == nil && sess != nil {
			session = sess
			if correlationID == "" {
				correlationID = sess.CorrelationID
			}
		}
	}
	if correlationID != "" {
		ctx = log.ContextWithCorrelationID(ctx, correlationID)
	}
	return correlationID, session, ctx, nil
}

func (o *Orchestrator) buildSessionContext(session *model.SessionRecord, e model.StartSessionEvent) (*sessionContext, error) {
	sessionMode := model.ModeLive
	if session.ContextData != nil {
		if raw := strings.TrimSpace(session.ContextData[model.CtxKeyMode]); raw != "" {
			sessionMode = strings.ToUpper(raw)
		}
	}
	if sessionMode != model.ModeLive && sessionMode != model.ModeRecording {
		sessionMode = model.ModeLive
	}

	playbackSource := e.ServiceRef
	if sessionMode == model.ModeRecording {
		if session.ContextData != nil {
			playbackSource = strings.TrimSpace(session.ContextData[model.CtxKeySource])
		}
		if playbackSource == "" {
			return nil, newReasonError(model.RInvariantViolation, "missing recording source", nil)
		}
	}

	return &sessionContext{
		Mode:       sessionMode,
		ServiceRef: playbackSource,
		IsVOD:      session.Profile.VOD || sessionMode == model.ModeRecording,
	}, nil
}

type leaseAcquisition struct {
	Slot                   int
	TunerLease             store.Lease
	TunerHandle            *pipelineLease.TunerLeaseHandle
	DedupLease             store.Lease
	RestrictedAccessHandle receiverusage.RestrictedAccessLeaseHandle
	HBCancel               context.CancelFunc
	HBCtx                  context.Context
	// ReleaseDedup / ReleaseTuner / ReleaseRestrictedAccess are in-memory release closures bound at acquisition time.
	ReleaseDedup            func()
	ReleaseTuner            func()
	ReleaseRestrictedAccess func() error
}
