package intents

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// SessionStore defines the minimal session persistence contract needed by intent processing.
type SessionStore interface {
	GetSession(ctx context.Context, id string) (*model.SessionRecord, error)
	PutSessionWithIdempotency(ctx context.Context, s *model.SessionRecord, idemKey string, ttl time.Duration) (existingID string, exists bool, err error)
}

// EventBus defines the event publication contract needed by intent processing.
type EventBus interface {
	Publish(ctx context.Context, topic string, evt any) error
}

// ChannelScanner resolves service capabilities used for profile selection.
type ChannelScanner interface {
	GetCapability(serviceRef string) (scan.Capability, bool)
}

// AdmissionController evaluates admission decisions.
type AdmissionController interface {
	Check(ctx context.Context, req admission.Request, state admission.RuntimeState) admission.Decision
}

// Deps defines external dependencies for the intent service.
type Deps interface {
	DVRWindow() time.Duration
	HasTunerSlots() bool
	SessionLeaseTTL() time.Duration
	SessionHeartbeatInterval() time.Duration
	SessionStore() SessionStore
	EventBus() EventBus
	ChannelScanner() ChannelScanner
	AdmissionController() AdmissionController
	AdmissionRuntimeState(ctx context.Context) admission.RuntimeState
	VerifyLivePlaybackDecision(token, principalID, serviceRef, playbackMode string) bool
	IncLivePlaybackKey(keyLabel, resultLabel string)
	RecordReject(code string)
	RecordAdmit()
	RecordIntent(intentType, mode, outcome string)
	RecordPublish(eventType, outcome string)
	RecordReplay(intentType string)
}
