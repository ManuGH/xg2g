package recordings

import (
	"context"
	"errors"
	"fmt"
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
	MarkProbed(serviceRef string, resolvedPath string, info *vod.StreamInfo, fp *vod.Fingerprint)
	Probe(ctx context.Context, path string) (*vod.StreamInfo, error)
	EnsureSpec(ctx context.Context, workDir, recordingID, source, cacheDir, name, finalPath string, profile vod.Profile) (vod.Spec, error)
}

// DurationPersistor is an orchestrator-level interface for persisting probe results.
// It explicitly extracts side-effects from the data-reading path (TruthProvider).
type DurationPersistor interface {
	PersistDuration(ctx context.Context, serviceRef string, duration int64) error
}

type PlaybackInfoResolver struct {
	cfg        *config.AppConfig
	vodManager MetadataManager
	engine     *playback.DecisionEngine
	truth      *truthProvider
	probeMgr   *probeManager
}

type ResolverOptions struct {
	DurationStore     DurationStore     // Used only by TruthProvider for READS
	DurationPersistor DurationPersistor // Used only by Orchestrator for WRITES
	PathResolver      PathResolver
	ProbeRootContext  context.Context
	ProbeFn           func(ctx context.Context, serviceRef, sourceURL string) (*vod.StreamInfo, error)
}

// NewResolver creates a new Resolver with strict invariant enforcement.
// It acts as a thin adapter, wiring up truthProvider and ProfileResolver.
func NewResolver(cfg *config.AppConfig, manager MetadataManager, opts ResolverOptions) (Resolver, error) {
	if cfg == nil {
		return nil, fmt.Errorf("NewResolver: cfg is nil")
	}
	if manager == nil {
		return nil, fmt.Errorf("NewResolver: manager is nil")
	}

	// 1. Build Truth Provider (Centralized Truth)
	truth, err := newTruthProvider(cfg, manager, opts)
	if err != nil {
		return nil, fmt.Errorf("NewResolver: truth provider: %w", err)
	}

	// 2. Build Profile Resolver
	profile := NewProfileResolver()

	pm := newProbeManager(opts.ProbeRootContext, manager, opts.ProbeFn, opts.DurationPersistor)

	r := &PlaybackInfoResolver{
		cfg:        cfg,
		vodManager: manager,
		truth:      truth,
		probeMgr:   pm,
	}

	// 3. Build Decision Engine (Orchestrated via r)
	engine := playback.NewDecisionEngine(r, profile)
	r.engine = engine

	return r, nil
}

// Resolve delegates to the Decision Engine and maps the result to the domain DTO.
func (r *PlaybackInfoResolver) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	// Construct PIDE Request
	protocolHint := ""
	if intent == "download" || intent == "direct" {
		protocolHint = "mp4"
	} else if intent == "hls" {
		protocolHint = "hls"
	} else {
		// V3Player live stream-info endpoint doesn't send "intent".
		// But earlier, the frontend says: "aber das ist ja ned fertige datei oder so wie bei plex oder" -> It expects direct play of VOD.
		// Let's pass "mp4" hint for "stream" intent on VOD if it's a file?
		// Engine will automatically fallback/transcode if unsupported.
		// Let's use "mp4" if they want plex-like direct streaming.
		protocolHint = "mp4"
	}

	req := playback.ResolveRequest{
		RecordingID:  serviceRef,
		ProtocolHint: protocolHint,
		Headers:      map[string]string{"X-Playback-Profile": string(profile)},
	}

	plan, err := r.engine.Resolve(ctx, req)
	if err != nil {
		// Map Engine Errors to Domain Errors
		if errors.Is(err, playback.ErrPreparing) {
			return r.resolvePreparing(ctx, serviceRef)
		}

		// For other errors, capture truth for status/reasons metadata (best effort)
		res := PlaybackInfoResult{}
		if truth, _ := r.truth.GetMediaTruth(ctx, serviceRef); truth.Status != "" {
			res.TruthStatus = string(truth.Status)
			res.ProbeState = string(truth.ProbeState)
			res.RetryAfter = truth.RetryAfter
			for _, rc := range truth.Reasons {
				res.TruthReasons = append(res.TruthReasons, string(rc))
			}
		}
		if errors.Is(err, playback.ErrForbidden) {
			return res, ErrForbidden{}
		}
		if errors.Is(err, playback.ErrNotFound) {
			return res, ErrNotFound{RecordingID: serviceRef}
		}

		// Legacy Error Mapping for Observability (Strict Type Checks)
		if errors.Is(err, ErrRemoteProbeUnsupported) {
			return res, ErrUpstream{Op: "probe_remote_unsupported", Cause: err}
		}
		if errors.Is(err, vod.ErrProbeCorrupt) || errors.Is(err, ErrProbeNotConfigured) || os.IsNotExist(err) || os.IsPermission(err) {
			// Engine propagates these raw errors via GetMediaTruth -> Resolve (error)
			return res, ErrUpstream{Op: "probe", Cause: err}
		}

		// Fail Closed Generic
		if errors.Is(err, playback.ErrUpstream) {
			// If engine generic error, we might want to wrap it
			return res, ErrUpstream{Op: "engine_decision", Cause: err}
		}

		// If it's a raw error falling through (e.g. probe ambiguous)
		return res, ErrUpstream{Op: "probe_ambiguous", Cause: err}
	}

	// Helper for pointer mapping
	s := func(v string) *string { return &v }
	i64 := func(v int64) *int64 { return &v }

	// Construct Result
	containerNorm := normalize.Token(plan.Container)
	isMP4 := containerNorm == "mp4" || containerNorm == "mov" || containerNorm == "m4v"
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
		Reason:     string(plan.DecisionReason), // Legacy string field
		Container:  s(plan.Container),
		VideoCodec: s(plan.VideoCodec),
		AudioCodec: s(plan.AudioCodec),
	}

	// Capture truth for status/reasons metadata
	if truth, err := r.truth.GetMediaTruth(ctx, serviceRef); err == nil {
		res.TruthStatus = string(truth.Status)
		res.ProbeState = string(truth.ProbeState)
		res.RetryAfter = truth.RetryAfter
		for _, rc := range truth.Reasons {
			res.TruthReasons = append(res.TruthReasons, string(rc))
		}
	}

	// Use Duration from PIDE Plan (authoritative)
	if plan.Duration > 0 {
		res.DurationSeconds = i64(int64(plan.Duration))
		res.MediaInfo.Duration = plan.Duration

		// Preserve canonical duration provenance when available.
		if plan.DurationSource != "" {
			ds := DurationSource(plan.DurationSource)
			if ds.Valid() {
				res.DurationSource = &ds
			}
		}
	}

	return res, nil
}

func (r *PlaybackInfoResolver) resolvePreparing(ctx context.Context, serviceRef string) (PlaybackInfoResult, error) {
	truth, err := r.GetMediaTruth(ctx, serviceRef)
	if err != nil {
		return PlaybackInfoResult{}, err
	}

	res := PlaybackInfoResult{
		Decision: playback.Decision{
			Mode:   playback.ModeDeny,
			Reason: playback.ReasonPreparing,
		},
		MediaInfo: playback.MediaInfo{
			// Status field removed as it doesn't exist in playback.MediaInfo
		},
		TruthStatus: string(truth.Status),
		ProbeState:  string(truth.ProbeState),
		RetryAfter:  truth.RetryAfter,
	}

	for _, rc := range truth.Reasons {
		res.TruthReasons = append(res.TruthReasons, string(rc))
	}

	return res, ErrPreparing{RecordingID: serviceRef}
}

func (r *PlaybackInfoResolver) GetMediaTruth(ctx context.Context, serviceRef string) (playback.MediaTruth, error) {
	truth, err := r.truth.GetMediaTruth(ctx, serviceRef)
	if err != nil {
		return truth, err
	}

	if truth.Status == playback.MediaStatusPreparing && r.probeMgr != nil {
		isDisabled := false
		for _, rc := range truth.Reasons {
			if rc == playback.ReasonCode(ReasonProbeDisabled) || rc == playback.ReasonCode(ReasonProbeUnsupported) {
				isDisabled = true
				break
			}
		}

		if !isDisabled {
			// Trigger idempotent orchestration
			_, source, localPath, _ := r.truth.ResolveSource(ctx, serviceRef)
			state, retryAfter := r.probeMgr.ensureProbed(ctx, serviceRef, source, localPath)
			truth.ProbeState = playback.ProbeState(state)
			truth.RetryAfter = retryAfter
		}
	}

	return truth, nil
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
