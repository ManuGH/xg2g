package recordings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/library"
	internalrecordings "github.com/ManuGH/xg2g/internal/recordings"
	"github.com/rs/zerolog/log"
)

// DurationStore abstracts duration persistence.
type DurationStore interface {
	GetDuration(ctx context.Context, rootID, relPath string) (seconds int64, ok bool, err error)
	SetDuration(ctx context.Context, rootID, relPath string, seconds int64) error
}

// PathResolver maps recording references to local paths and library coordinates.
type PathResolver interface {
	ResolveRecordingPath(serviceRef string) (localPath, rootID, relPath string, err error)
}

// TruthProvider manages the central "Version of Truth" for recordings.
// It performs classification only (no probe scheduling side-effects).
type TruthProvider struct {
	cfg             *config.AppConfig
	vodManager      MetadataManager
	durationStore   DurationStore
	pathResolver    PathResolver
	probeFn         func(ctx context.Context, sourceURL string) error
	probeConfigured bool
}

const (
	remoteProbeErrorPrefix = "remote_probe_failed: "
)

type ProbeHint struct {
	Kind            string
	Source          string
	LocalPath       string
	RootID          string
	RelPath         string
	StoreKnownEmpty bool
}

type MediaTruthOutcome struct {
	Truth      playback.MediaTruth
	NeedsProbe bool
	ProbeHint  ProbeHint
}

// NewTruthProvider creates a new TruthProvider with strict invariant enforcement.
func NewTruthProvider(cfg *config.AppConfig, manager MetadataManager, opts ResolverOptions) (*TruthProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("NewTruthProvider: cfg is nil")
	}
	if manager == nil {
		return nil, fmt.Errorf("NewTruthProvider: manager is nil")
	}

	probe := opts.ProbeFn
	probeConfigured := true
	if probe == nil {
		probeConfigured = false
		probe = func(ctx context.Context, sourceURL string) error {
			return ErrProbeNotConfigured
		}
	}

	return &TruthProvider{
		cfg:             cfg,
		vodManager:      manager,
		durationStore:   opts.DurationStore,
		pathResolver:    opts.PathResolver,
		probeFn:         probe,
		probeConfigured: probeConfigured,
	}, nil
}

// GetMediaTruth implements playback.MediaTruthProvider.
// Side-effect free: classification only.
func (t *TruthProvider) GetMediaTruth(ctx context.Context, serviceRef string) (playback.MediaTruth, error) {
	outcome, err := t.GetMediaTruthOutcome(ctx, serviceRef)
	return outcome.Truth, err
}

// GetMediaTruthOutcome classifies playback truth and optional probe requirements.
// Contract: no side-effects (no probe scheduling, no metadata mutations).
func (t *TruthProvider) GetMediaTruthOutcome(ctx context.Context, serviceRef string) (MediaTruthOutcome, error) {
	kind, source, _, err := t.resolveSource(ctx, serviceRef)
	if err != nil {
		log.Warn().Err(err).Str("sref", serviceRef).Msg("GetMediaTruth: resolveSource failed")
		out := MediaTruthOutcome{
			Truth: playback.MediaTruth{State: playback.StateFailed},
		}
		if errors.As(err, &ErrNotFound{}) {
			return out, playback.ErrNotFound
		}
		return out, playback.ErrUpstream
	}
	log.Info().Str("sref", serviceRef).Str("kind", kind).Str("source", source).Msg("GetMediaTruth: source resolved")

	// 0. Job State Gate (Active Build?)
	cacheDir, err := RecordingCacheDir(t.cfg.HLS.Root, serviceRef)
	if err == nil {
		if status, exists := t.vodManager.Get(ctx, cacheDir); exists {
			if status.State == vod.JobStateBuilding || status.State == vod.JobStateFinalizing {
				return MediaTruthOutcome{
					Truth: playback.MediaTruth{
						State:             playback.StatePreparing,
						ProbeState:        playback.ProbeStateInFlight,
						RetryAfterSeconds: playback.RetryAfterPreparingDefault,
					},
				}, nil
			}
		}
	}

	// 1. Resolve Local Path
	var localPath string
	var rootID, relPath string
	if t.pathResolver != nil {
		resolvedPath, rID, rRel, pathErr := t.pathResolver.ResolveRecordingPath(serviceRef)
		if pathErr == nil && resolvedPath != "" {
			localPath = resolvedPath
			rootID = rID
			relPath = rRel
		}
	}
	// Fallback to source if file scheme
	if localPath == "" && strings.HasPrefix(source, "file://") {
		if u, _ := url.Parse(source); u != nil {
			localPath = u.Path
		} else {
			localPath = strings.TrimPrefix(source, "file://")
		}
	}

	// 2. Duration Store Lookup (Precedence: Store > Cache)
	var storeDuration int64
	var storeKnownEmpty bool
	if t.durationStore != nil && rootID != "" && relPath != "" {
		dur, ok, err := t.durationStore.GetDuration(ctx, rootID, relPath)
		if err == nil {
			if ok && dur > 0 {
				storeDuration = dur
			} else {
				storeKnownEmpty = true
			}
		}
	}

	// 3. Check Metadata Cache
	meta, metaOk := t.vodManager.GetMetadata(serviceRef)

	if metaOk {
		switch meta.State {
		case vod.ArtifactStateMissing:
			return MediaTruthOutcome{Truth: failedTruthFromMetadata(meta)}, playback.ErrNotFound
		case vod.ArtifactStatePreparing:
			return MediaTruthOutcome{
				Truth: playback.MediaTruth{
					State:             playback.StatePreparing,
					ProbeState:        playback.ProbeStateInFlight,
					RetryAfterSeconds: playback.RetryAfterPreparingDefault,
				},
			}, nil
		case vod.ArtifactStateFailed:
			// Keep local hard failures as terminal, but do not collapse unknown/unprobed
			// remote paths into a synthetic upstream error.
			if strings.HasPrefix(meta.Error, remoteProbeErrorPrefix) {
				return MediaTruthOutcome{Truth: failedTruthFromMetadata(meta)}, playback.ErrUpstream
			}
			if localPath == "" && !meta.HasArtifact() && !meta.HasPlaylist() {
				outcome := MediaTruthOutcome{
					Truth: playback.MediaTruth{
						State: playback.StatePreparing,
					},
				}
				if t.probeConfigured {
					outcome.NeedsProbe = true
					outcome.ProbeHint = ProbeHint{
						Kind:            kind,
						Source:          source,
						LocalPath:       localPath,
						RootID:          rootID,
						RelPath:         relPath,
						StoreKnownEmpty: storeKnownEmpty,
					}
					outcome.Truth.RetryAfterSeconds = playback.RetryAfterPreparingDefault
				}
				if !t.probeConfigured {
					outcome.Truth.ProbeState = playback.ProbeStateBlocked
					outcome.Truth.ProbeBlockedReason = playback.ProbeBlockedReasonDisabled
					outcome.Truth.RetryAfterSeconds = playback.RetryAfterPreparingBlockedDefault
				}
				return outcome, nil
			}
			return MediaTruthOutcome{Truth: failedTruthFromMetadata(meta)}, playback.ErrUpstream
		}
	}

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
		outcome := MediaTruthOutcome{
			Truth: playback.MediaTruth{
				State: playback.StatePreparing,
			},
		}
		// Only request probe scheduling when an actual probe path exists.
		if localPath != "" || t.probeConfigured {
			outcome.NeedsProbe = true
			outcome.ProbeHint = ProbeHint{
				Kind:            kind,
				Source:          source,
				LocalPath:       localPath,
				RootID:          rootID,
				RelPath:         relPath,
				StoreKnownEmpty: storeKnownEmpty,
			}
			outcome.Truth.RetryAfterSeconds = playback.RetryAfterPreparingDefault
		}
		if localPath == "" && !t.probeConfigured {
			outcome.Truth.ProbeState = playback.ProbeStateBlocked
			outcome.Truth.ProbeBlockedReason = playback.ProbeBlockedReasonDisabled
			outcome.Truth.RetryAfterSeconds = playback.RetryAfterPreparingBlockedDefault
		}
		return outcome, nil
	}

	durationInput := DurationTruthResolveInput{
		PrimaryDurationSeconds:   storeDuration,
		SecondaryDurationSeconds: meta.Duration,
		SecondarySource:          durationSecondarySourceFromMetadata(meta),
	}
	durationTruth := ResolveDurationTruth(durationInput)
	finalDuration := float64(0)
	if sec := durationTruth.DurationSeconds(); sec != nil {
		finalDuration = float64(*sec)
	}

	// Return Truth
	return MediaTruthOutcome{
		Truth: playback.MediaTruth{
			State:              playback.StateReady,
			Container:          meta.Container,
			VideoCodec:         meta.VideoCodec,
			AudioCodec:         meta.AudioCodec,
			Duration:           finalDuration,
			DurationSource:     string(durationTruth.Source),
			DurationConfidence: string(durationTruth.Confidence),
			DurationReasons:    durationTruth.ReasonStrings(),
			Width:              meta.Width,
			Height:             meta.Height,
			FPS:                meta.FPS,
			Interlaced:         meta.Interlaced,
		},
	}, nil
}

func failedTruthFromMetadata(meta vod.Metadata) playback.MediaTruth {
	out := playback.MediaTruth{
		State:      playback.StateFailed,
		Container:  meta.Container,
		VideoCodec: meta.VideoCodec,
		AudioCodec: meta.AudioCodec,
		Width:      meta.Width,
		Height:     meta.Height,
		FPS:        meta.FPS,
		Interlaced: meta.Interlaced,
	}
	if meta.Duration > 0 {
		out.Duration = float64(meta.Duration)
	}
	return out
}

// resolveSource determines protocol and address
func (t *TruthProvider) resolveSource(ctx context.Context, serviceRef string) (kind, source, name string, err error) {
	_ = ctx
	receiverPath := internalrecordings.ExtractPathFromServiceRef(serviceRef)
	policy := t.cfg.RecordingPlaybackPolicy
	allowLocal := policy != config.PlaybackPolicyReceiverOnly
	allowReceiver := policy != config.PlaybackPolicyLocalOnly

	if allowLocal {
		mapper := internalrecordings.NewPathMapper(t.cfg.RecordingPathMappings)
		if localPath, ok := mapper.ResolveLocalExisting(receiverPath); ok {
			return "local", (&url.URL{Scheme: "file", Path: localPath}).String(), filepath.Base(localPath), nil
		}
	}
	if allowReceiver {
		baseURL, err := url.Parse(t.cfg.Enigma2.BaseURL)
		if err != nil {
			return "", "", "", err
		}
		u := *baseURL
		u.Host = fmt.Sprintf("%s:%d", baseURL.Hostname(), t.cfg.Enigma2.StreamPort)
		if t.cfg.Enigma2.Username != "" {
			u.User = url.UserPassword(t.cfg.Enigma2.Username, t.cfg.Enigma2.Password)
		}
		u.RawQuery = ""
		u.Fragment = ""
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

func durationSecondarySourceFromMetadata(meta vod.Metadata) DurationTruthSource {
	// If we have a final artifact path, duration can be treated as container-derived.
	if meta.ArtifactPath != "" {
		return DurationTruthSourceContainer
	}
	return DurationTruthSourceFFProbe
}

// --- Library Adapters (Moved from resolver.go) ---

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
	if rp, err := filepath.EvalSymlinks(localPath); err == nil {
		localPath = filepath.Clean(rp)
	}

	var bestRoot *library.RootConfig
	var bestRootResolved string
	longestPrefix := -1
	for i := range roots {
		root := &roots[i]
		cleanRoot := filepath.Clean(root.Path)
		rootResolved := cleanRoot
		if rr, err := filepath.EvalSymlinks(cleanRoot); err == nil {
			rootResolved = filepath.Clean(rr)
		}

		if hasPathPrefix(localPath, rootResolved) {
			if len(rootResolved) > longestPrefix {
				longestPrefix = len(rootResolved)
				bestRoot = root
				bestRootResolved = rootResolved
			}
		}
	}
	if bestRoot == nil {
		return "", "", false
	}
	// Use the resolved root path to avoid macOS /var vs /private/var mismatches.
	rel, err := filepath.Rel(bestRootResolved, localPath)
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
