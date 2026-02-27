package recordings

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/normalize"
)

// Resolver interface in domain.
type Resolver interface {
	Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error)
	GetMediaTruth(ctx context.Context, serviceRef string) (playback.MediaTruth, error)
}

var ErrProbeNotConfigured = errors.New("probe not configured")
var ErrRemoteProbeUnsupported = errors.New("remote probe cannot determine codecs; local mapping or metadata required")

// MetadataManager abstracts the VOD manager for metadata operations.
type MetadataManager interface {
	Get(ctx context.Context, dir string) (*vod.JobStatus, bool)
	GetMetadata(serviceRef string) (vod.Metadata, bool)
	MarkFailed(serviceRef string, reason string)
	MarkFailure(serviceRef string, state vod.ArtifactState, reason string, resolvedPath string, fp *vod.Fingerprint)
	MarkProbed(serviceRef string, resolvedPath string, info *vod.StreamInfo, fp *vod.Fingerprint)
	Probe(ctx context.Context, path string) (*vod.StreamInfo, error)
	EnsureSpec(ctx context.Context, workDir, recordingID, source, cacheDir, name, finalPath string, profile vod.Profile) (vod.Spec, error)
}

type PlaybackInfoResolver struct {
	cfg        *config.AppConfig
	vodManager MetadataManager
	engine     *playback.DecisionEngine
}

type ResolverOptions struct {
	DurationStore DurationStore
	PathResolver  PathResolver
	ProbeFn       func(ctx context.Context, sourceURL string) error
}

// NewResolver creates a new Resolver with strict invariant enforcement.
// It acts as a thin adapter, wiring up TruthProvider and ProfileResolver.
func NewResolver(cfg *config.AppConfig, manager MetadataManager, opts ResolverOptions) (Resolver, error) {
	if cfg == nil {
		return nil, fmt.Errorf("NewResolver: cfg is nil")
	}
	if manager == nil {
		return nil, fmt.Errorf("NewResolver: manager is nil")
	}

	// 1. Build Truth Provider (Centralized Truth)
	truth, err := NewTruthProvider(cfg, manager, opts)
	if err != nil {
		return nil, fmt.Errorf("NewResolver: truth provider: %w", err)
	}

	// 2. Build Profile Resolver
	profile := NewProfileResolver()

	// 3. Build Decision Engine
	engine := playback.NewDecisionEngine(truth, profile)

	return &PlaybackInfoResolver{
		cfg:        cfg,
		vodManager: manager,
		engine:     engine,
	}, nil
}

// Resolve delegates to the Decision Engine and maps the result to the domain DTO.
func (r *PlaybackInfoResolver) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	// Construct PIDE Request
	req := playback.ResolveRequest{
		RecordingID: serviceRef,
		Headers:     map[string]string{"X-Playback-Profile": string(profile)},
	}

	plan, err := r.engine.Resolve(ctx, req)
	if err != nil {
		// Map Engine Errors to Domain Errors
		if errors.Is(err, playback.ErrPreparing) {
			return PlaybackInfoResult{}, ErrPreparing{RecordingID: serviceRef}
		}
		if errors.Is(err, playback.ErrForbidden) {
			return PlaybackInfoResult{}, ErrForbidden{}
		}
		if errors.Is(err, playback.ErrNotFound) {
			return PlaybackInfoResult{}, ErrNotFound{RecordingID: serviceRef}
		}

		// Legacy Error Mapping for Observability (Strict Type Checks)
		if errors.Is(err, ErrRemoteProbeUnsupported) {
			return PlaybackInfoResult{}, ErrUpstream{Op: "probe_remote_unsupported", Cause: err}
		}
		if errors.Is(err, vod.ErrProbeCorrupt) || errors.Is(err, ErrProbeNotConfigured) || os.IsNotExist(err) || os.IsPermission(err) {
			// Engine propagates these raw errors via GetMediaTruth -> Resolve (error)
			return PlaybackInfoResult{}, ErrUpstream{Op: "probe", Cause: err}
		}

		// Fail Closed Generic
		if errors.Is(err, playback.ErrUpstream) {
			// If engine generic error, we might want to wrap it
			return PlaybackInfoResult{}, ErrUpstream{Op: "engine_decision", Cause: err}
		}

		// If it's a raw error falling through (e.g. probe ambiguous)
		return PlaybackInfoResult{}, ErrUpstream{Op: "probe_ambiguous", Cause: err}
	}

	// Helper for pointer mapping
	s := func(v string) *string { return &v }
	i64 := func(v int64) *int64 { return &v }

	// Construct Result
	containerNorm := normalize.Token(plan.Container)
	isMP4 := containerNorm == "mp4" || containerNorm == "mov" || containerNorm == "m4v"
	durationTruth := durationTruthFromPlan(plan)
	res := PlaybackInfoResult{
		Decision: playback.Decision{
			Mode:     plan.Mode,
			Artifact: mapProtocolToArtifact(plan.Protocol),
			Reason:   plan.DecisionReason,
		},
		MediaInfo: playback.MediaInfo{
			Container:             plan.Container,
			VideoCodec:            plan.VideoCodec,
			AudioCodec:            plan.AudioCodec,
			IsMP4FastPathEligible: isMP4, // PIDE Protocol Check acts as eligibility gate
		},
		Reason:        string(plan.DecisionReason), // Legacy string field
		DurationTruth: durationTruth,
		Container:     s(plan.Container),
		VideoCodec:    s(plan.VideoCodec),
		AudioCodec:    s(plan.AudioCodec),
	}

	if sec := durationTruth.DurationSeconds(); sec != nil && *sec > 0 {
		res.DurationSeconds = i64(*sec)
		res.MediaInfo.Duration = float64(*sec)
	}
	if durationTruth.Source.Valid() && durationTruth.Source != DurationTruthSourceUnknown {
		ds := DurationSource(durationTruth.Source)
		res.DurationSource = &ds
	}

	return res, nil
}

func (r *PlaybackInfoResolver) GetMediaTruth(ctx context.Context, serviceRef string) (playback.MediaTruth, error) {
	return r.engine.GetMediaTruth(ctx, serviceRef)
}

func mapProtocolToArtifact(p playback.Protocol) playback.ArtifactKind {
	switch p {
	case playback.ProtocolHLS:
		return playback.ArtifactHLS
	case playback.ProtocolMP4:
		return playback.ArtifactMP4
	default:
		return playback.ArtifactNone
	}
}

func durationTruthFromPlan(plan playback.PlaybackPlan) DurationTruth {
	input := DurationTruthResolveInput{}
	source := DurationTruthSource(plan.DurationSource)
	durationSec := int64(math.Round(plan.Duration))

	switch source {
	case DurationTruthSourceMetadata:
		input.PrimaryDurationSeconds = durationSec
	case DurationTruthSourceFFProbe, DurationTruthSourceContainer:
		input.SecondaryDurationSeconds = durationSec
		input.SecondarySource = source
	case DurationTruthSourceHeuristic:
		input.AllowHeuristic = true
		input.HeuristicDurationSeconds = durationSec
	default:
		// If source is absent/unknown but duration exists, treat as secondary best-effort.
		if durationSec > 0 {
			input.SecondaryDurationSeconds = durationSec
		}
	}

	for _, reason := range plan.DurationReasons {
		if reason == string(DurationReasonProbeFailed) {
			input.SecondaryFailed = true
			break
		}
	}

	out := ResolveDurationTruth(input)
	if conf := DurationTruthConfidence(plan.DurationConfidence); conf.Valid() {
		out.Confidence = conf
	}

	if len(plan.DurationReasons) > 0 {
		existing := make(map[DurationReasonCode]bool, len(out.Reasons))
		for _, reason := range out.Reasons {
			existing[reason] = true
		}
		for _, raw := range plan.DurationReasons {
			reason := DurationReasonCode(raw)
			if reason.Valid() && !existing[reason] {
				out.Reasons = append(out.Reasons, reason)
			}
		}
		sortDurationReasonsByPriority(out.Reasons)
	}

	return out
}
