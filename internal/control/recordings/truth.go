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

// --- Truth Outcomes (R1/R5) ---

type TruthStatus string

const (
	TruthStatusReady               TruthStatus = "ready"
	TruthStatusPreparing           TruthStatus = "preparing"
	TruthStatusNotFound            TruthStatus = "not_found"
	TruthStatusUpstreamUnavailable TruthStatus = "upstream_unavailable"
)

type ReasonCode string

const (
	ReasonReady               ReasonCode = "ready"
	ReasonProbeQueued         ReasonCode = "probe_queued"
	ReasonProbeInFlight       ReasonCode = "probe_in_flight"
	ReasonProbeBlocked        ReasonCode = "probe_blocked"
	ReasonProbeBackoff        ReasonCode = "probe_backoff"
	ReasonProbeDisabled       ReasonCode = "probe_disabled"
	ReasonNotFound            ReasonCode = "not_found"
	ReasonInvalidID           ReasonCode = "invalid_id"
	ReasonUpstreamFailure     ReasonCode = "upstream_failure"
	ReasonReceiverUnreachable ReasonCode = "receiver_unreachable"
	ReasonProbeFailed         ReasonCode = "probe_failed"
)

type TruthOutcome struct {
	Status     TruthStatus
	Reasons    []ReasonCode
	Truth      *playback.MediaTruth // nil unless Status == Ready
	RetryAfter int
	ProbeState string
}

func (o TruthOutcome) ToMediaTruth() playback.MediaTruth {
	var t playback.MediaTruth
	t.Status = playback.MediaStatus(o.Status)
	t.RetryAfter = o.RetryAfter
	t.ProbeState = o.ProbeState

	if o.Status == TruthStatusReady && o.Truth != nil {
		readyTruth := *o.Truth
		readyTruth.Status = playback.MediaStatusReady
		t = readyTruth
	}

	// Map domain codes to playback codes (identity mapping since both are strings)
	t.Reasons = make([]playback.ReasonCode, len(o.Reasons))
	for i, r := range o.Reasons {
		t.Reasons[i] = playback.ReasonCode(r)
	}

	return t
}

var ErrInvalidRecordingID = errors.New("invalid recording ID")

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
// It coordinates metadata cache, duration store, and active probing.
type TruthProvider struct {
	cfg             *config.AppConfig
	vodManager      MetadataManager
	durationStore   DurationStore
	pathResolver    PathResolver
	probeConfigured bool
}

// NewTruthProvider creates a new TruthProvider with strict invariant enforcement.
func NewTruthProvider(cfg *config.AppConfig, manager MetadataManager, opts ResolverOptions) (*TruthProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("NewTruthProvider: cfg is nil")
	}
	if manager == nil {
		return nil, fmt.Errorf("NewTruthProvider: manager is nil")
	}

	return &TruthProvider{
		cfg:             cfg,
		vodManager:      manager,
		durationStore:   opts.DurationStore,
		pathResolver:    opts.PathResolver,
		probeConfigured: opts.ProbeFn != nil,
	}, nil
}

// GetMediaTruth implements playback.MediaTruthProvider.
// It centralizes the precedence logic: Job > Store > Cache > Probe.
func (t *TruthProvider) GetMediaTruth(ctx context.Context, serviceRef string) (playback.MediaTruth, error) {
	outcome := t.GetMediaTruthOutcome(ctx, serviceRef)
	return outcome.ToMediaTruth(), nil
}

// GetMediaTruthOutcome evaluates the current state of truth without side effects (R1).
func (t *TruthProvider) GetMediaTruthOutcome(ctx context.Context, serviceRef string) TruthOutcome {
	kind, source, _, err := t.resolveSource(ctx, serviceRef)
	if err != nil {
		log.Warn().Err(err).Str("sref", serviceRef).Msg("GetMediaTruthOutcome: resolveSource failed")
		if errors.As(err, &ErrNotFound{}) {
			return TruthOutcome{Status: TruthStatusNotFound, Reasons: []ReasonCode{ReasonNotFound}}
		}
		return TruthOutcome{Status: TruthStatusUpstreamUnavailable, Reasons: []ReasonCode{ReasonUpstreamFailure}}
	}
	log.Debug().Str("sref", serviceRef).Str("kind", kind).Str("source", source).Msg("GetMediaTruthOutcome: source resolved")

	// 0. Job State Gate (Active Build?)
	cacheDir, err := RecordingCacheDir(t.cfg.HLS.Root, serviceRef)
	if err == nil {
		if status, exists := t.vodManager.Get(ctx, cacheDir); exists {
			if status.State == vod.JobStateBuilding || status.State == vod.JobStateFinalizing {
				return TruthOutcome{Status: TruthStatusPreparing, Reasons: []ReasonCode{ReasonProbeInFlight}}
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
	if t.durationStore != nil && rootID != "" && relPath != "" {
		dur, ok, err := t.durationStore.GetDuration(ctx, rootID, relPath)
		if err == nil && ok && dur > 0 {
			storeDuration = dur
		}
	}

	// 3. Check Metadata Cache
	meta, metaOk := t.vodManager.GetMetadata(serviceRef)

	// Gate: Terminal Failure preventing re-probe
	if metaOk && meta.State == vod.ArtifactStateFailed {
		return TruthOutcome{Status: TruthStatusUpstreamUnavailable, Reasons: []ReasonCode{ReasonProbeFailed}}
	}

	// Gate: Impossible Probe (Fail Fast)
	// If we have no known artifacts/playlist and no way to get them, fail now.
	if !metaOk || (!meta.HasArtifact() && !meta.HasPlaylist()) {
		if localPath == "" {
			if !t.probeConfigured {
				// No local path + No probe configured = Cannot ever succeed
				return TruthOutcome{Status: TruthStatusPreparing, Reasons: []ReasonCode{ReasonProbeDisabled}}
			}
		}
	}

	codecComplete := metaOk && meta.Container != "" && meta.VideoCodec != "" && meta.AudioCodec != ""

	needsProbe := false
	if !codecComplete {
		needsProbe = true
	} else if storeDuration <= 0 && meta.Duration <= 0 {
		needsProbe = true
	}

	if needsProbe {
		// Valid metadata exists, but we are missing critical codec/container truth.
		// Pure classification: return status and indicate that probe is required.
		return TruthOutcome{
			Status:  TruthStatusPreparing,
			Reasons: []ReasonCode{ReasonProbeQueued},
		}
	}

	// Determine Final Truth Duration
	finalDuration := float64(meta.Duration)
	if storeDuration > 0 {
		finalDuration = float64(storeDuration)
	}

	// Return Truth (Gate R1 Invariant: Status == Ready => Truth != nil)
	return TruthOutcome{
		Status:  TruthStatusReady,
		Reasons: []ReasonCode{ReasonReady},
		Truth: &playback.MediaTruth{
			Status:     playback.MediaStatusReady,
			Reasons:    []playback.ReasonCode{playback.ReasonCode(ReasonReady)},
			Container:  meta.Container,
			VideoCodec: meta.VideoCodec,
			AudioCodec: meta.AudioCodec,
			Duration:   finalDuration,
			Width:      meta.Width,
			Height:     meta.Height,
			FPS:        meta.FPS,
			Interlaced: meta.Interlaced,
		},
	}
}

// resolveSource determines protocol and address
func (t *TruthProvider) resolveSource(ctx context.Context, serviceRef string) (kind, source, name string, err error) {
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
