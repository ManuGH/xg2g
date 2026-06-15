package manager

import (
	"context"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	"strings"
	"time"
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
	Slot       int
	TunerLease store.Lease
	DedupLease store.Lease
	HBCancel   context.CancelFunc
	HBCtx      context.Context
	// ReleaseDedup / ReleaseTuner are in-memory release closures bound at acquisition time.
	// They are the AUTHORITATIVE release path for the session this process started: they do
	// not depend on the ContextData tuner-slot mirror (B) being persisted, so they cover the
	// window where start fails after the lease is acquired but before transitionStarting
	// commits B. Both default to no-ops so an unacquired lease releases cleanly.
	ReleaseDedup func()
	ReleaseTuner func()
}
