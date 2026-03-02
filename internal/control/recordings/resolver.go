package recordings

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
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
	truth      *TruthProvider
	sf         singleflight.Group
	probeTTL   time.Duration
	probeMu    sync.Mutex
	lastProbe  map[string]time.Time
	backoff    map[string]time.Time
}

type ResolverOptions struct {
	DurationStore DurationStore
	PathResolver  PathResolver
	ProbeFn       func(ctx context.Context, sourceURL string) error
}

const defaultResolverProbeTriggerTTL = 5 * time.Second

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
		truth:      truth,
		probeTTL:   defaultResolverProbeTriggerTTL,
		lastProbe:  make(map[string]time.Time),
		backoff:    make(map[string]time.Time),
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
			r.ensureProbeForPreparing(ctx, serviceRef)
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
	if r.truth == nil {
		return r.engine.GetMediaTruth(ctx, serviceRef)
	}
	outcome, err := r.truth.GetMediaTruthOutcome(ctx, serviceRef)
	if err != nil {
		return outcome.Truth, err
	}
	if outcome.NeedsProbe {
		ps, blockedReason, retryAfter := r.scheduleProbe(serviceRef, outcome.ProbeHint)
		if ps != playback.ProbeStateUnknown {
			outcome.Truth.ProbeState = ps
		}
		if blockedReason != playback.ProbeBlockedReasonNone {
			outcome.Truth.ProbeBlockedReason = blockedReason
		}
		if retryAfter > 0 {
			outcome.Truth.RetryAfterSeconds = retryAfter
		}
	}
	if outcome.Truth.State == playback.StatePreparing && outcome.Truth.ProbeState == playback.ProbeStateUnknown {
		outcome.Truth.ProbeState = playback.ProbeStateInFlight
		if outcome.Truth.RetryAfterSeconds <= 0 {
			outcome.Truth.RetryAfterSeconds = playback.RetryAfterPreparingDefault
		}
	}
	return outcome.Truth, nil
}

func (r *PlaybackInfoResolver) ensureProbeForPreparing(ctx context.Context, serviceRef string) {
	if r.truth == nil {
		return
	}
	outcome, err := r.truth.GetMediaTruthOutcome(ctx, serviceRef)
	if err != nil {
		return
	}
	if outcome.NeedsProbe {
		_, _, _ = r.scheduleProbe(serviceRef, outcome.ProbeHint)
	}
}

func (r *PlaybackInfoResolver) shouldTriggerProbeNow(key string, now time.Time) bool {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()

	last, ok := r.lastProbe[key]
	if ok && now.Sub(last) < r.probeTTL {
		return false
	}
	r.lastProbe[key] = now
	return true
}

func (r *PlaybackInfoResolver) scheduleProbe(serviceRef string, hint ProbeHint) (playback.ProbeState, playback.ProbeBlockedReason, int) {
	key := hashSingleflightKey(hint.Kind, hint.Source)
	now := time.Now()
	if until := r.currentBackoffUntil(key); now.Before(until) {
		remaining := int(until.Sub(now).Seconds())
		if remaining <= 0 {
			remaining = playback.RetryAfterPreparingBlockedDefault
		}
		return playback.ProbeStateBlocked, playback.ProbeBlockedReasonBackoff, remaining
	}

	if !r.shouldTriggerProbeNow(key, now) {
		return playback.ProbeStateInFlight, playback.ProbeBlockedReasonNone, playback.RetryAfterPreparingDefault
	}

	go func() {
		_, _, _ = r.sf.Do(hashSingleflightKey(hint.Kind, hint.Source), func() (interface{}, error) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			if hint.LocalPath == "" && !r.truth.probeConfigured {
				return nil, nil
			}

			if hint.LocalPath == "" {
				if err := r.truth.probeFn(bgCtx, hint.Source); err != nil {
					if errors.Is(err, ErrProbeNotConfigured) || errors.Is(err, ErrRemoteProbeUnsupported) {
						return nil, nil
					}
					log.Warn().Err(err).Str("source", hint.Source).Msg("remote probe failed")
					r.vodManager.MarkFailed(serviceRef, remoteProbeErrorPrefix+err.Error())
					r.markBackoff(key, time.Now().Add(playback.RetryAfterPreparingBlockedDefault*time.Second))
					return nil, err
				}
				r.clearBackoff(key)
				return nil, nil
			}

			info, err := r.vodManager.Probe(bgCtx, hint.LocalPath)
			if err != nil {
				log.Warn().Err(err).Str("path", hint.LocalPath).Msg("local probe failed")
				r.vodManager.MarkFailed(serviceRef, err.Error())
				r.markBackoff(key, time.Now().Add(playback.RetryAfterPreparingBlockedDefault*time.Second))
				return nil, err
			}
			if info == nil {
				log.Warn().Str("path", hint.LocalPath).Msg("local probe returned no media info")
				r.vodManager.MarkFailed(serviceRef, vod.ErrProbeCorrupt.Error())
				r.markBackoff(key, time.Now().Add(playback.RetryAfterPreparingBlockedDefault*time.Second))
				return nil, vod.ErrProbeCorrupt
			}

			r.vodManager.MarkProbed(serviceRef, hint.LocalPath, info, nil)
			r.clearBackoff(key)

			dur := int64(math.Round(info.Video.Duration))
			if dur > 0 && r.truth.durationStore != nil && hint.RootID != "" && hint.RelPath != "" && hint.StoreKnownEmpty {
				_ = r.truth.durationStore.SetDuration(bgCtx, hint.RootID, hint.RelPath, dur)
			}

			return nil, nil
		})
	}()

	return playback.ProbeStateQueued, playback.ProbeBlockedReasonNone, playback.RetryAfterPreparingDefault
}

func (r *PlaybackInfoResolver) currentBackoffUntil(key string) time.Time {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	return r.backoff[key]
}

func (r *PlaybackInfoResolver) markBackoff(key string, until time.Time) {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	r.backoff[key] = until
}

func (r *PlaybackInfoResolver) clearBackoff(key string) {
	r.probeMu.Lock()
	defer r.probeMu.Unlock()
	delete(r.backoff, key)
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
