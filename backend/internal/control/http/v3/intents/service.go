package intents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"strings"
	"time"
)

const admissionLeaseTTL = 30 * time.Second

const startReplayRecoveryAttempts = 3

// Service handles intent processing independent of HTTP transport.
type Service struct {
	deps Deps
}

type startHardwareState struct {
	hasGPU      bool
	defaultGPU  profiles.GPUBackend
	av1Backend  profiles.GPUBackend
	hevcBackend profiles.GPUBackend
	h264Backend profiles.GPUBackend
}

type startProfileResolution struct {
	requestedPlaybackMode string
	publicRequestProfile  string
	effectiveProfileID    string
	profileSpec           model.ProfileSpec
	sourceProfile         *playbackprofile.SourceProfile
	operatorSnapshot      profiles.OperatorOverrideSnapshot
	hostPressureBand      playbackprofile.HostPressureBand
	hostOverrideApplied   bool
	bucket                string
	idempotencyKey        string
	resolvedIntent        string
	degradedFrom          string
	autoCodecTrace        autocodec.SelectionTrace
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) ProcessIntent(ctx context.Context, intent Intent) (*Result, *Error) {
	switch intent.Type {
	case model.IntentTypeStreamStart:
		return s.processStart(ctx, intent)
	case model.IntentTypeStreamStop:
		return s.processStop(ctx, intent)
	default:
		return nil, &Error{Kind: ErrorInvalidInput, Message: "unsupported intent type"}
	}
}

func (s *Service) processStart(ctx context.Context, intent Intent) (*Result, *Error) {
	intent.ClientCaps = normalizedClientCaps(intent.ClientCaps)
	store := s.deps.SessionStore()
	bus := s.deps.EventBus()
	// Watchpoint: start intents may use scan capability as a profile hint source
	// (for example interlaced/progressive handling), but they are not a second
	// SSOT for live container/codec readiness. Any future readiness/media-truth
	// branching must go through the live truth resolver used by /live/stream-info.
	capability := s.lookupStartCapability(intent.ServiceRef)
	hardwareState := detectStartHardwareState()
	hwaccelMode, err := s.resolveStartHWAccelMode(intent, hardwareState)
	if err != nil {
		return nil, err
	}
	reqProfileID, requestedPlaybackMode, err := s.resolveRequestedStartProfile(ctx, intent, hwaccelMode, capability)
	if err != nil {
		return nil, err
	}
	resolution, err := s.resolveStartProfile(ctx, intent, capability, hardwareState, hwaccelMode, reqProfileID, requestedPlaybackMode)
	if err != nil {
		return nil, err
	}
	if err := s.checkStartAdmission(ctx, intent, resolution.profileSpec); err != nil {
		return nil, err
	}

	hwaccelEffective, hwaccelReason, encoderBackend := deriveStartHWAccelSummary(resolution.profileSpec, hwaccelMode, hardwareState.hasGPU)
	s.logStartProfileResolution(intent, resolution, hardwareState, hwaccelMode, hwaccelEffective, hwaccelReason, encoderBackend)

	if !s.deps.HasTunerSlots() {
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "no_slots")
		return nil, &Error{Kind: ErrorNoTunerSlots, Message: "no tuner slots configured", RetryAfter: "10"}
	}

	phaseLabel := "phase2"
	session := s.buildStartSession(intent, resolution)
	if replay, err := s.persistStartSession(ctx, intent, store, session, resolution.idempotencyKey, phaseLabel); err != nil {
		return nil, err
	} else if replay != nil {
		return replay, nil
	}
	if err := s.publishStartSession(ctx, intent, bus, resolution.effectiveProfileID, phaseLabel); err != nil {
		return nil, err
	}

	intent.Logger.Info().Msg("intent accepted")
	s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "accepted")

	return &Result{
		SessionID:     intent.SessionID,
		Status:        "accepted",
		CorrelationID: intent.CorrelationID,
	}, nil
}

func (s *Service) processStop(ctx context.Context, intent Intent) (*Result, *Error) {
	bus := s.deps.EventBus()
	event := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     intent.SessionID,
		Reason:        model.RClientStop,
		CorrelationID: intent.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if err := bus.Publish(ctx, string(model.EventStopSession), event); err != nil {
		intent.Logger.Error().Err(err).Msg("failed to publish stop event")
		s.deps.RecordPublish("session.stop", "error")
		s.deps.RecordIntent(string(model.IntentTypeStreamStop), "any", "publish_error")
		return nil, &Error{Kind: ErrorPublishUnavailable, Message: "failed to dispatch intent", Cause: err}
	}
	s.deps.RecordPublish("session.stop", "ok")
	s.deps.RecordIntent(string(model.IntentTypeStreamStop), "any", "accepted")

	return &Result{
		SessionID:     intent.SessionID,
		Status:        "accepted",
		CorrelationID: intent.CorrelationID,
	}, nil
}

// ComputeIdemKey generates a deterministic SHA256 idempotency key.
func ComputeIdemKey(intentType model.IntentType, ref, profile, bucket string) string {
	payload := fmt.Sprintf("v1:%s:%s:%s:%s", intentType, ref, profile, bucket)
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}

func resolvePlaybackDecisionToken(requestToken string, params map[string]string) (token, keyLabel, resultLabel string, err error) {
	canonicalToken := strings.TrimSpace(requestToken)
	paramToken := strings.TrimSpace(params["playback_decision_token"])
	paramID := strings.TrimSpace(params["playback_decision_id"])

	switch {
	case canonicalToken == "":
		switch {
		case paramToken == "" && paramID == "":
			return "", "none", "rejected_missing", fmt.Errorf("playbackDecisionToken is required when playback_mode is provided")
		default:
			return "", "params_only", "rejected_missing", fmt.Errorf("playbackDecisionToken is required when playback_mode is provided")
		}
	case paramToken != "" && paramToken != canonicalToken:
		return "", "request+playback_decision_token", "mismatch", fmt.Errorf("params.playback_decision_token must match playbackDecisionToken")
	case paramID != "" && paramID != canonicalToken:
		return "", "request+playback_decision_id", "mismatch", fmt.Errorf("params.playback_decision_id must match playbackDecisionToken")
	case paramToken != "" && paramID != "":
		return canonicalToken, "all", "equal", nil
	case paramToken != "":
		return canonicalToken, "request+playback_decision_token", "equal", nil
	case paramID != "":
		return canonicalToken, "request+playback_decision_id", "equal", nil
	default:
		return canonicalToken, "request", "accepted", nil
	}
}
