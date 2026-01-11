package recordings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/library"
	internalrecordings "github.com/ManuGH/xg2g/internal/recordings"
	"golang.org/x/sync/singleflight"
)

// Resolver interface in domain.
// Resolver interface in domain.
type Resolver interface {
	Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error)
}

// DurationStore abstracts duration persistence from the resolver.
type DurationStore interface {
	GetDuration(ctx context.Context, rootID, relPath string) (seconds int64, ok bool, err error)
	SetDuration(ctx context.Context, rootID, relPath string, seconds int64) error
}

// PathResolver maps recording references to local paths and library coordinates.
type PathResolver interface {
	ResolveRecordingPath(serviceRef string) (localPath, rootID, relPath string, err error)
}

type DefaultResolver struct {
	cfg           *config.AppConfig
	vodManager    *vod.Manager
	durationStore DurationStore
	pathResolver  PathResolver
	Probe         func(ctx context.Context, sourceURL string) error
	sf            singleflight.Group
}

func NewResolver(cfg *config.AppConfig, manager *vod.Manager) *DefaultResolver {
	return &DefaultResolver{
		cfg:        cfg,
		vodManager: manager,
	}
}

func (r *DefaultResolver) WithDurationStore(store DurationStore, pathResolver PathResolver) *DefaultResolver {
	r.durationStore = store
	r.pathResolver = pathResolver
	return r
}

func (r *DefaultResolver) WithProbe(probe func(context.Context, string) error) *DefaultResolver {
	r.Probe = probe
	return r
}

func (r *DefaultResolver) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {

	kind, source, name, err := r.resolveSource(ctx, serviceRef)
	if err != nil {
		if errors.Is(err, ErrNotFound{}) {
			return PlaybackInfoResult{}, err
		}
		return PlaybackInfoResult{}, ErrUpstream{Op: "resolveSource", Cause: err}
	}

	var storeDuration int64
	var durationReason string
	var localPath string
	var rootID string
	var relPath string

	if r.durationStore != nil && r.pathResolver != nil {
		resolvedPath, resolvedRootID, resolvedRel, pathErr := r.pathResolver.ResolveRecordingPath(serviceRef)
		if resolvedPath != "" {
			localPath = resolvedPath
		}
		if pathErr == nil {
			rootID = resolvedRootID
			relPath = resolvedRel
			if dur, ok, _ := r.durationStore.GetDuration(ctx, rootID, relPath); ok && dur > 0 {
				storeDuration = dur
				durationReason = "resolved_via_store"
			}
		}
	}

	if localPath == "" && strings.HasPrefix(source, "file://") {
		localPath = strings.TrimPrefix(source, "file://")
	}

	cacheDir, err := RecordingCacheDir(r.cfg.HLS.Root, serviceRef)
	if err != nil {
		return PlaybackInfoResult{}, ErrUpstream{Op: "RecordingCacheDir", Cause: err}
	}

	if status, exists := r.vodManager.Get(ctx, cacheDir); exists {
		if status.State == vod.JobStateBuilding || status.State == vod.JobStateFinalizing {
			return PlaybackInfoResult{}, ErrPreparing{RecordingID: serviceRef}
		}
	}

	meta, metaOk := r.vodManager.GetMetadata(serviceRef)
	needsProbe := storeDuration == 0 && (!metaOk || meta.Duration <= 0)

	if needsProbe {
		sfKey := hashSingleflightKey(kind, source)
		val, err, _ := r.sf.Do(sfKey, func() (interface{}, error) {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if localPath != "" {
				info, err := r.vodManager.Probe(probeCtx, localPath)
				if err != nil {
					return nil, err
				}
				if info == nil {
					return nil, vod.ErrProbeCorrupt
				}
				duration := int64(math.Round(info.Video.Duration))
				if duration <= 0 {
					return nil, vod.ErrProbeCorrupt
				}
				return vod.Metadata{
					ResolvedPath: localPath,
					Duration:     duration,
					Container:    info.Container,
					VideoCodec:   info.Video.CodecName,
					AudioCodec:   info.Audio.CodecName,
				}, nil
			}

			if r.Probe != nil {
				if err := r.Probe(probeCtx, source); err != nil {
					return nil, err
				}
			}

			container := "mpegts"
			if strings.HasSuffix(strings.ToLower(source), ".mp4") {
				container = "mp4"
			}
			return vod.Metadata{
				Container:  container,
				VideoCodec: "h264",
				AudioCodec: "aac",
			}, nil
		})

		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return PlaybackInfoResult{}, ErrPreparing{RecordingID: serviceRef}
			}
			return PlaybackInfoResult{}, ErrUpstream{Op: "probe", Cause: err}
		}

		meta = val.(vod.Metadata)
		if meta.ResolvedPath == "" {
			meta.ResolvedPath = localPath
		}
		r.vodManager.UpdateMetadata(serviceRef, meta)

		if storeDuration == 0 && r.durationStore != nil && rootID != "" && relPath != "" && meta.Duration > 0 {
			_ = r.durationStore.SetDuration(ctx, rootID, relPath, meta.Duration)
			durationReason = "probed_and_persisted"
		}
	}

	if !metaOk && storeDuration > 0 {
		container := "mpegts"
		if localPath != "" && strings.HasSuffix(strings.ToLower(localPath), ".mp4") {
			container = "mp4"
		}
		meta = vod.Metadata{
			ResolvedPath: localPath,
			Duration:     storeDuration,
			Container:    container,
			VideoCodec:   "h264",
			AudioCodec:   "aac",
		}
	}

	finalDuration := float64(meta.Duration)
	if storeDuration > 0 {
		finalDuration = float64(storeDuration)
	}
	if durationReason == "" {
		durationReason = "resolved_via_metadata"
	}

	info := playback.MediaInfo{
		AbsPath:    source,
		Container:  meta.Container,
		VideoCodec: meta.VideoCodec,
		AudioCodec: meta.AudioCodec,
		Duration:   finalDuration,
	}

	clientProfile := mapProfile(profile)
	decision, err := playback.Decide(clientProfile, info, playback.Policy{})
	if err != nil {
		return PlaybackInfoResult{}, ErrUpstream{Op: "decide", Cause: err}
	}

	if decision.Mode == playback.ModeTranscode && decision.Artifact == playback.ArtifactHLS {
		playlistName := "index.m3u8"
		if kind == "receiver" {
			playlistName = "index.live.m3u8"
		}
		finalPath := filepath.Join(cacheDir, playlistName)
		_, err := r.vodManager.EnsureSpec(ctx, cacheDir, serviceRef, source, cacheDir, name, finalPath, vod.ProfileDefault)
		if err != nil {
			return PlaybackInfoResult{}, ErrUpstream{Op: "EnsureSpec", Cause: err}
		}
	}

	return PlaybackInfoResult{
		Decision:  decision,
		MediaInfo: info,
		Reason:    durationReason,
	}, nil
}

func (r *DefaultResolver) resolveSource(ctx context.Context, serviceRef string) (kind, source, name string, err error) {
	receiverPath := internalrecordings.ExtractPathFromServiceRef(serviceRef)

	policy := r.cfg.RecordingPlaybackPolicy
	allowLocal := policy != config.PlaybackPolicyReceiverOnly
	allowReceiver := policy != config.PlaybackPolicyLocalOnly

	if allowLocal {
		mapper := internalrecordings.NewPathMapper(r.cfg.RecordingPathMappings)
		if localPath, ok := mapper.ResolveLocalExisting(receiverPath); ok {
			return "local", "file://" + localPath, filepath.Base(localPath), nil
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

func mapProfile(p PlaybackProfile) playback.ClientProfile {
	switch p {
	case ProfileSafari:
		return playback.ClientProfile{IsSafari: true}
	case ProfileTVOS:
		return playback.ClientProfile{CanPlayTS: true, CanPlayAC3: true}
	default:
		return playback.ClientProfile{}
	}
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
