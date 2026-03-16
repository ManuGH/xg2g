package recordings

import (
	"context"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// ResolvePlaybackInfo resolves the truthful playback inputs up to the decision engine boundary.
func (s *Service) ResolvePlaybackInfo(ctx context.Context, req PlaybackInfoRequest) (PlaybackInfoResult, *PlaybackInfoError) {
	svc := s.deps.RecordingsService()
	if svc == nil {
		return PlaybackInfoResult{}, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorUnavailable,
			Message: "Recordings service is not initialized",
		}
	}

	sourceRef, truth, err := s.resolveSubjectTruth(ctx, req, svc)
	if err != nil {
		return PlaybackInfoResult{}, err
	}

	cfg := s.deps.Config()
	operatorPolicy := cfg.Playback.Operator
	operatorRuleName := ""
	operatorRuleScope := ""
	if req.SubjectKind == PlaybackSubjectRecording {
		effectiveOperator, matchedRule := profiles.ResolveEffectivePlaybackOperatorConfig(cfg.Playback.Operator, profiles.OperatorRuleModeRecording, sourceRef)
		operatorPolicy = effectiveOperator
		if matchedRule != nil {
			operatorRuleName = strings.TrimSpace(matchedRule.Name)
			operatorRuleScope = profiles.NormalizeOperatorRuleMode(matchedRule.Mode)
		}
	}

	resolvedCaps := domainrecordings.ResolveCapabilities(
		ctx,
		req.PrincipalID,
		req.APIVersion,
		req.RequestedProfile,
		req.Headers,
		req.Capabilities,
	)

	input := buildDecisionInput(
		req,
		truth,
		resolvedCaps,
		cfg,
		operatorPolicy,
		s.deps.HostPressure(ctx),
	)

	_, dec, prob := decision.Decide(ctx, input, req.SchemaType)
	if prob != nil {
		return PlaybackInfoResult{}, &PlaybackInfoError{
			Kind: PlaybackInfoErrorProblem,
			Problem: &PlaybackInfoProblem{
				Status: prob.Status,
				Type:   prob.Type,
				Title:  prob.Title,
				Code:   prob.Code,
				Detail: prob.Detail,
			},
		}
	}

	return PlaybackInfoResult{
		SourceRef:            sourceRef,
		Truth:                truth,
		ResolvedCapabilities: resolvedCaps,
		Decision:             dec,
		ClientProfile:        req.ClientProfile,
		OperatorRuleName:     operatorRuleName,
		OperatorRuleScope:    operatorRuleScope,
	}, nil
}

func (s *Service) resolveSubjectTruth(ctx context.Context, req PlaybackInfoRequest, svc RecordingsService) (string, playback.MediaTruth, *PlaybackInfoError) {
	subjectID := strings.TrimSpace(req.SubjectID)
	if subjectID == "" {
		return "", playback.MediaTruth{}, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInvalidInput,
			Message: "subject id is required",
		}
	}

	switch req.SubjectKind {
	case PlaybackSubjectLive:
		if err := domainrecordings.ValidateLiveRef(subjectID); err != nil {
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorInvalidInput,
				Message: "serviceRef must be a valid live Enigma2 reference",
				Cause:   err,
			}
		}
		return subjectID, assumeLiveTruth(subjectID), nil
	case PlaybackSubjectRecording:
		sourceRef, ok := domainrecordings.DecodeRecordingID(subjectID)
		if !ok {
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorInvalidInput,
				Message: "Invalid recording ID format",
			}
		}

		truth, err := svc.GetMediaTruth(ctx, subjectID)
		if err != nil {
			return "", playback.MediaTruth{}, classifyPlaybackInfoError(err)
		}

		switch truth.Status {
		case playback.MediaStatusPreparing:
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:              PlaybackInfoErrorPreparing,
				Message:           "Retry shortly.",
				RetryAfterSeconds: truth.RetryAfter,
				ProbeState:        string(truth.ProbeState),
			}
		case playback.MediaStatusUpstreamUnavailable:
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorUpstreamUnavailable,
				Message: "Retry later.",
			}
		case playback.MediaStatusNotFound:
			return "", playback.MediaTruth{}, &PlaybackInfoError{
				Kind:    PlaybackInfoErrorNotFound,
				Message: "recording not found",
			}
		}
		return sourceRef, truth, nil
	default:
		return "", playback.MediaTruth{}, &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInvalidInput,
			Message: "unsupported playback subject kind",
		}
	}
}

func classifyPlaybackInfoError(err error) *PlaybackInfoError {
	class := domainrecordings.Classify(err)
	msg := err.Error()

	switch class {
	case domainrecordings.ClassInvalidArgument:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorInvalidInput, Message: msg, Cause: err}
	case domainrecordings.ClassForbidden:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorForbidden, Message: msg, Cause: err}
	case domainrecordings.ClassNotFound:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorNotFound, Message: msg, Cause: err}
	case domainrecordings.ClassPreparing:
		return &PlaybackInfoError{
			Kind:              PlaybackInfoErrorPreparing,
			Message:           msg,
			RetryAfterSeconds: 5,
			ProbeState:        string(playback.ProbeStateInFlight),
			Cause:             err,
		}
	case domainrecordings.ClassUnsupported:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorUnsupported, Message: msg, Cause: err}
	case domainrecordings.ClassUpstream:
		return &PlaybackInfoError{Kind: PlaybackInfoErrorUpstreamUnavailable, Message: msg, Cause: err}
	default:
		return &PlaybackInfoError{
			Kind:    PlaybackInfoErrorInternal,
			Message: "An unexpected error occurred",
			Cause:   err,
		}
	}
}

func buildDecisionInput(
	req PlaybackInfoRequest,
	truth playback.MediaTruth,
	resolvedCaps capabilities.PlaybackCapabilities,
	cfg config.AppConfig,
	operatorPolicy config.PlaybackOperatorConfig,
	hostPressure playbackprofile.HostPressureAssessment,
) decision.DecisionInput {
	serverCanTranscode := strings.TrimSpace(cfg.FFmpeg.Bin) != "" && strings.TrimSpace(cfg.HLS.Root) != ""
	clientAllowsTranscode := resolvedCaps.AllowTranscode == nil || *resolvedCaps.AllowTranscode
	allowTranscode := serverCanTranscode && clientAllowsTranscode

	return decision.DecisionInput{
		RequestID:       req.RequestID,
		RequestedIntent: playbackprofile.NormalizeRequestedIntent(req.RequestedProfile),
		APIVersion:      req.APIVersion,
		Source: decision.Source{
			Container:  truth.Container,
			VideoCodec: truth.VideoCodec,
			AudioCodec: truth.AudioCodec,
			Width:      truth.Width,
			Height:     truth.Height,
			FPS:        truth.FPS,
		},
		Capabilities: decision.FromCapabilities(resolvedCaps),
		Policy: decision.Policy{
			AllowTranscode: allowTranscode,
			Operator: decision.OperatorPolicy{
				ForceIntent:    playbackprofile.NormalizeRequestedIntent(operatorPolicy.ForceIntent),
				MaxQualityRung: playbackprofile.NormalizeQualityRung(operatorPolicy.MaxQualityRung),
			},
			Host: decision.HostPolicy{
				PressureBand: playbackprofile.NormalizeHostPressureBand(string(hostPressure.EffectiveBand)),
			},
		},
	}
}
