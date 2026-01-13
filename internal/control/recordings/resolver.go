package recordings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/library"
	internalrecordings "github.com/ManuGH/xg2g/internal/recordings"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// Resolver interface in domain.
type Resolver interface {
	Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error)
}

var ErrProbeNotConfigured = errors.New("probe not configured")
var ErrRemoteProbeUnsupported = errors.New("remote probe cannot determine codecs; local mapping or metadata required")

// DurationStore abstracts duration persistence from the resolver.
type DurationStore interface {
	GetDuration(ctx context.Context, rootID, relPath string) (seconds int64, ok bool, err error)
	SetDuration(ctx context.Context, rootID, relPath string, seconds int64) error
}

// PathResolver maps recording references to local paths and library coordinates.
type PathResolver interface {
	ResolveRecordingPath(serviceRef string) (localPath, rootID, relPath string, err error)
}

// MetadataManager abstracts the VOD manager for metadata operations.
type MetadataManager interface {
	Get(ctx context.Context, dir string) (*vod.JobStatus, bool)
	GetMetadata(serviceRef string) (vod.Metadata, bool)
	UpdateMetadata(serviceRef string, meta vod.Metadata)
	Probe(ctx context.Context, path string) (*vod.StreamInfo, error)
	EnsureSpec(ctx context.Context, workDir, recordingID, source, cacheDir, name, finalPath string, profile vod.Profile) (vod.Spec, error)
}

type DefaultResolver struct {
	cfg           *config.AppConfig
	vodManager    MetadataManager
	durationStore DurationStore
	pathResolver  PathResolver
	Probe         func(ctx context.Context, sourceURL string) error
	sf            singleflight.Group
	engine        *playback.DecisionEngine
}

type ResolverOptions struct {
	DurationStore DurationStore
	PathResolver  PathResolver
	ProbeFn       func(ctx context.Context, sourceURL string) error
}

func NewResolver(cfg *config.AppConfig, manager MetadataManager, opts ResolverOptions) *DefaultResolver {
	probe := opts.ProbeFn
	if probe == nil {
		probe = func(ctx context.Context, sourceURL string) error {
			return ErrProbeNotConfigured
		}
	}
	r := &DefaultResolver{
		cfg:           cfg,
		vodManager:    manager,
		durationStore: opts.DurationStore,
		pathResolver:  opts.PathResolver,
		Probe:         probe,
	}

	// Inject PIDE Engine
	// We use a small adapter for Profile Resolution to avoid method collision
	profResolver := &HeaderProfileResolver{}
	r.engine = playback.NewDecisionEngine(r, profResolver)
	return r
}

// Ensure DefaultResolver implements MediaTruthProvider (but NOT ClientProfileResolver directly)
var _ playback.MediaTruthProvider = (*DefaultResolver)(nil)

// HeaderProfileResolver implements playback.ClientProfileResolver
type HeaderProfileResolver struct{}

func (h *HeaderProfileResolver) Resolve(ctx context.Context, headers map[string]string) (playback.ClientProfile, error) {
	// Extract profile alias from synthetic header
	profileName := headers["X-Playback-Profile"]

	var p playback.ClientProfile
	p.Name = profileName
	p.UserAgent = headers["User-Agent"]

	// Map Profile Name to Capabilities
	switch PlaybackProfile(profileName) {
	case ProfileSafari:
		p.Name = "safari_native"
		p.IsSafari = true
		p.SupportsNativeHLS = true
		p.SupportsH264 = true
		p.SupportsAAC = true
		p.SupportsAC3 = true
	case ProfileTVOS:
		p.Name = "tvos"
		p.IsSafari = true
		p.SupportsNativeHLS = true
		p.SupportsH264 = true
		p.SupportsAAC = true
		p.SupportsAC3 = true
		p.CanPlayTS = true
	case ProfileGeneric:
		p.Name = "mse_hlsjs"
		p.SupportsMSE = true
		p.SupportsH264 = true
		p.SupportsAAC = true
		p.IsChrome = true
	default:
		p.Name = "unknown"
		p.SupportsMSE = true
		p.SupportsH264 = true
	}
	return p, nil
}

// Resolve delegates to the Decision Engine.
func (r *DefaultResolver) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
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
	isMP4 := plan.Container == "mp4" || plan.Container == "mov"
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

	// Use Duration from PIDE Plan (authoritative)
	if plan.Duration > 0 {
		res.DurationSeconds = i64(int64(plan.Duration))
		res.MediaInfo.Duration = plan.Duration

		// Map Source for legacy observability (best effort)
		if meta, ok := r.vodManager.GetMetadata(serviceRef); ok {
			if float64(meta.Duration) != plan.Duration {
				ds := DurationSourceStore
				res.DurationSource = &ds
			} else {
				ds := DurationSourceCache
				res.DurationSource = &ds
			}
		}
	}

	return res, nil
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

// --- PIDE Interface Implementations ---

// GetMediaTruth implements playback.MediaTruthProvider.
func (r *DefaultResolver) GetMediaTruth(ctx context.Context, serviceRef string) (playback.MediaTruth, error) {
	kind, source, _, err := r.resolveSource(ctx, serviceRef)
	if err != nil {
		if errors.As(err, &ErrNotFound{}) {
			return playback.MediaTruth{}, playback.ErrNotFound
		}
		return playback.MediaTruth{}, playback.ErrUpstream
	}

	// 0. Job State Gate
	cacheDir, err := RecordingCacheDir(r.cfg.HLS.Root, serviceRef)
	if err == nil {
		if status, exists := r.vodManager.Get(ctx, cacheDir); exists {
			if status.State == vod.JobStateBuilding || status.State == vod.JobStateFinalizing {
				return playback.MediaTruth{State: playback.StatePreparing}, nil
			}
		}
	}

	// 1. Resolve Local Path
	var localPath string
	var rootID, relPath string
	if r.pathResolver != nil {
		resolvedPath, rID, rRel, pathErr := r.pathResolver.ResolveRecordingPath(serviceRef)
		if pathErr == nil && resolvedPath != "" {
			localPath = resolvedPath
			rootID = rID
			relPath = rRel
		}
	}
	if localPath == "" && strings.HasPrefix(source, "file://") {
		if u, _ := url.Parse(source); u != nil {
			localPath = u.Path
		} else {
			localPath = strings.TrimPrefix(source, "file://")
		}
	}

	// 2. Duration Store Lookup (Precedence)
	var storeDuration int64
	var storeKnownEmpty bool
	if r.durationStore != nil && rootID != "" && relPath != "" {
		dur, ok, err := r.durationStore.GetDuration(ctx, rootID, relPath)
		if err == nil {
			if ok && dur > 0 {
				storeDuration = dur
			} else {
				storeKnownEmpty = true
			}
		}
	}

	// 3. Check Metadata Cache
	meta, metaOk := r.vodManager.GetMetadata(serviceRef)
	codecComplete := metaOk && meta.Container != "" && meta.VideoCodec != "" && meta.AudioCodec != ""

	// Needs Probe?
	// Rule: If we lack Codecs -> Probe.
	// Rule: If we lack Duration AND Store missed -> Probe.
	needsProbe := false
	if !codecComplete {
		needsProbe = true
	} else if storeDuration <= 0 && meta.Duration <= 0 {
		needsProbe = true
	}

	if needsProbe {
		// Model B: Trigger Async Probe and Return Preparing immediately.
		// We use singleflight to ensure only one actual probe happens per source.
		// The caller receives StatePreparing (HTTP 202/503) and should retry.
		go func() {
			_, _, _ = r.sf.Do(hashSingleflightKey(kind, source), func() (interface{}, error) {
				// Use a detached context for background work
				bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()

				var probedMeta vod.Metadata
				var probeErr error

				// Local Probe
				if localPath != "" {
					info, err := r.vodManager.Probe(bgCtx, localPath)
					if err != nil {
						probeErr = err
					} else if info == nil {
						probeErr = vod.ErrProbeCorrupt
					} else {
						probedMeta = vod.Metadata{
							ResolvedPath: localPath,
							Duration:     int64(math.Round(info.Video.Duration)),
							Container:    info.Container,
							VideoCodec:   info.Video.CodecName,
							AudioCodec:   info.Audio.CodecName,
						}
					}
				} else {
					// Remote Probe
					if err := r.Probe(bgCtx, source); err != nil {
						probeErr = err
					} else {
						// Remote probe success but no data returned (legacy behavior?)
						// In original code, it returned ErrRemoteProbeUnsupported.
						// We keep failing closed for remote probe without codecs.
						probeErr = ErrRemoteProbeUnsupported
					}
				}

				if probeErr != nil {
					log.Warn().Err(probeErr).Str("source", source).Msg("async probe failed")
					return nil, probeErr
				}

				// Success: Update Metadata
				r.vodManager.UpdateMetadata(serviceRef, probedMeta)

				// Success: Update Store if valid duration and store was empty
				if probedMeta.Duration > 0 && r.durationStore != nil && rootID != "" && relPath != "" && storeKnownEmpty {
					_ = r.durationStore.SetDuration(bgCtx, rootID, relPath, probedMeta.Duration)
				}

				return nil, nil
			})
		}()

		// Return StatePreparing immediately so GET is not blocked.
		return playback.MediaTruth{State: playback.StatePreparing}, nil
	}

	// Determine Final Truth Duration
	finalDuration := float64(meta.Duration)
	if storeDuration > 0 {
		finalDuration = float64(storeDuration)
	}

	// Return Truth
	return playback.MediaTruth{
		State:      playback.StateReady,
		Container:  meta.Container,
		VideoCodec: meta.VideoCodec,
		AudioCodec: meta.AudioCodec,
		Duration:   finalDuration,
	}, nil
}

// --- Rest of File (Helpers) ---

func (r *DefaultResolver) resolveSource(ctx context.Context, serviceRef string) (kind, source, name string, err error) {
	receiverPath := internalrecordings.ExtractPathFromServiceRef(serviceRef)
	policy := r.cfg.RecordingPlaybackPolicy
	allowLocal := policy != config.PlaybackPolicyReceiverOnly
	allowReceiver := policy != config.PlaybackPolicyLocalOnly

	if allowLocal {
		mapper := internalrecordings.NewPathMapper(r.cfg.RecordingPathMappings)
		if localPath, ok := mapper.ResolveLocalExisting(receiverPath); ok {
			return "local", (&url.URL{Scheme: "file", Path: localPath}).String(), filepath.Base(localPath), nil
		}
	}
	if allowReceiver {
		baseURL, err := url.Parse(r.cfg.Enigma2.BaseURL)
		if err != nil {
			return "", "", "", err
		}
		u := *baseURL
		u.Host = fmt.Sprintf("%s:%d", baseURL.Hostname(), r.cfg.Enigma2.StreamPort)
		if r.cfg.Enigma2.Username != "" {
			u.User = url.UserPassword(r.cfg.Enigma2.Username, r.cfg.Enigma2.Password)
		}
		u.Path = "/" + serviceRef
		u.RawPath = "/" + EscapeServiceRefPath(serviceRef)
		return "receiver", u.String(), "", nil
	}
	return "", "", "", ErrNotFound{RecordingID: serviceRef}
}

func hashSingleflightKey(kind, source string) string {
	sum := sha256.Sum256([]byte(kind + "|" + source))
	return hex.EncodeToString(sum[:])
}

// --- Library Adapters ---

type LibraryDurationStore struct {
	store *library.Store
}

func NewLibraryDurationStore(store *library.Store) DurationStore {
	return &LibraryDurationStore{store: store}
}

func (s *LibraryDurationStore) GetDuration(ctx context.Context, rootID, relPath string) (int64, bool, error) {
	if s == nil || s.store == nil {
		return 0, false, nil
	}
	item, err := s.store.GetItem(ctx, rootID, relPath)
	if err != nil {
		return 0, false, err
	}
	if item == nil || item.DurationSeconds <= 0 {
		return 0, false, nil
	}
	return item.DurationSeconds, true, nil
}

func (s *LibraryDurationStore) SetDuration(ctx context.Context, rootID, relPath string, seconds int64) error {
	if s == nil || s.store == nil {
		return errors.New("library store not configured")
	}
	return s.store.UpdateItemDuration(ctx, rootID, relPath, seconds)
}

type LibraryPathResolver struct {
	mapper internalrecordings.Mapper
	roots  []library.RootConfig
}

func NewLibraryPathResolver(mapper internalrecordings.Mapper, roots []library.RootConfig) PathResolver {
	return &LibraryPathResolver{
		mapper: mapper,
		roots:  roots,
	}
}

func (r *LibraryPathResolver) ResolveRecordingPath(serviceRef string) (string, string, string, error) {
	if r == nil || r.mapper == nil {
		return "", "", "", errors.New("recording path mapper not configured")
	}
	receiverPath := internalrecordings.ExtractPathFromServiceRef(serviceRef)
	localPath, ok := r.mapper.ResolveLocalExisting(receiverPath)
	if !ok || localPath == "" {
		return "", "", "", errors.New("recording path not mapped")
	}
	rootID, relPath, ok := matchLibraryRoot(localPath, r.roots)
	if !ok {
		return localPath, "", "", errors.New("recording path not in library roots")
	}
	return localPath, rootID, relPath, nil
}

func matchLibraryRoot(localPath string, roots []library.RootConfig) (string, string, bool) {
	localPath = filepath.Clean(localPath)
	var bestRoot *library.RootConfig
	longestPrefix := -1
	for i := range roots {
		root := &roots[i]
		cleanRoot := filepath.Clean(root.Path)
		if hasPathPrefix(localPath, cleanRoot) {
			if len(cleanRoot) > longestPrefix {
				longestPrefix = len(cleanRoot)
				bestRoot = root
			}
		}
	}
	if bestRoot == nil {
		return "", "", false
	}
	rel, err := filepath.Rel(bestRoot.Path, localPath)
	if err != nil {
		return "", "", false
	}
	if rel == "." {
		rel = ""
	}
	return bestRoot.ID, rel, true
}

func hasPathPrefix(p, root string) bool {
	p = filepath.Clean(p)
	root = filepath.Clean(root)
	if p == root {
		return true
	}
	rootWithSep := root
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}
	return strings.HasPrefix(p, rootWithSep)
}
