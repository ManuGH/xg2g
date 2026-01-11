package v3

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ManuGH/xg2g/internal/control/http/v3/types"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/library"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// DefaultVODResolver provides the production implementation of VOD resolution.
// It enforces the "Duration Truth" priority: Manager Cache > Library Store > Probe.
type DefaultVODResolver struct {
	vodMgr     *vod.Manager
	libStore   *library.Store
	pathMapper recordings.Mapper
	roots      []library.RootConfig

	clock         vod.Clock
	sf            singleflight.Group
	supervisorCtx context.Context
	mu            sync.Mutex
	active        map[string]*vodActivity
}

type vodActivity struct {
	waiters int32
	cancel  context.CancelFunc
	ctx     context.Context
}

func NewVODResolver(ctx context.Context, vodMgr *vod.Manager, libStore *library.Store, pathMapper recordings.Mapper, roots []library.RootConfig, clock vod.Clock) *DefaultVODResolver {
	return &DefaultVODResolver{
		supervisorCtx: ctx,
		vodMgr:        vodMgr,
		libStore:      libStore,
		pathMapper:    pathMapper,
		roots:         roots,
		clock:         clock,
		active:        make(map[string]*vodActivity),
	}
}

func (r *DefaultVODResolver) ResolveVOD(ctx context.Context, recordingID string, intent types.PlaybackIntent, profile playback.ClientProfile) (playback.MediaInfo, error) {
	serviceRef := r.decodeID(recordingID)
	if serviceRef == "" {
		return playback.MediaInfo{}, fmt.Errorf("invalid recording id")
	}

	// Refined singleflight key: recordingID | intent
	key := fmt.Sprintf("%s|%s", recordingID, intent)

	// Capture waiters before starting/joining singleflight for robust observability
	ictx, waiters := r.incInterest(key)
	defer r.decInterest(key)

	// 1. Primary: In-Memory Cache (Manager)
	if meta, exists := r.vodMgr.GetMetadata(serviceRef); exists {
		if meta.State == vod.ArtifactStateReady && meta.Duration > 0 && meta.Container != "" {
			log.Debug().
				Str("recording_id", recordingID).
				Str("duration_source", "cache").
				Int32("waiters", waiters).
				Msg("VOD duration resolved via cache")
			return r.mapMediaInfo(meta), nil
		}
		// Negative Cache check
		if meta.State == vod.ArtifactStateFailed {
			if r.isWithinNegativeTTL(meta) {
				return playback.MediaInfo{}, fmt.Errorf("recording failed: %s", meta.FailureKind)
			}
			// TTL expired, continue to re-probe
		}
	}

	// 2. Secondary: Library Store (Persistence)
	receiverPath := recordings.ExtractPathFromServiceRef(serviceRef)
	localPath, ok := r.pathMapper.ResolveLocalExisting(receiverPath)
	if !ok {
		return playback.MediaInfo{}, ErrRecordingNotFound
	}

	item, _ := r.findLibraryItem(ctx, localPath)
	// Store check ignored for Facts because it lacks Container/Codecs.
	// Proceed to Probe.

	// 3. Tertiary: Probe (One-time, protected by singleflight)
	resCh := r.sf.DoChan(key, func() (interface{}, error) {
		info, err := r.vodMgr.Probe(ictx, localPath)
		if err != nil {
			// Persist Negative Cache
			r.persistFailure(serviceRef, err)
			return nil, err
		}

		duration := int64(math.Round(info.Video.Duration))
		if duration <= 0 {
			err := vod.ErrProbeCorrupt
			r.persistFailure(serviceRef, err)
			return nil, err
		}

		meta := vod.Metadata{
			State:        vod.ArtifactStateReady,
			ResolvedPath: localPath,
			Duration:     duration,
			Container:    info.Container,
			VideoCodec:   info.Video.CodecName,
			AudioCodec:   info.Audio.CodecName,
			UpdatedAt:    r.clock.Now().Unix(),
		}

		// Persist to Manager
		r.vodMgr.UpdateMetadata(serviceRef, meta)

		// Persist to Store (if matched to a root)
		if item != nil {
			log.Info().Str("root", item.RootID).Str("rel", item.RelPath).Msg("persisting VOD duration to store (via existing item)")
			if err := r.libStore.UpdateItemDuration(r.supervisorCtx, item.RootID, item.RelPath, duration); err != nil {
				log.Error().Err(err).Msg("failed to persist VOD duration to store (existing item)")
			}
		} else if matchedRootID, matchedRel, found := r.matchToRoots(localPath); found {
			log.Info().Str("root", matchedRootID).Str("rel", matchedRel).Msg("persisting VOD duration to store (via path match)")
			if err := r.libStore.UpdateItemDuration(r.supervisorCtx, matchedRootID, matchedRel, duration); err != nil {
				log.Error().Err(err).Msg("failed to persist VOD duration to store (path match)")
			}
		} else {
			log.Warn().Str("path", localPath).Msg("could not match VOD path to any library root for persistence")
		}

		return meta, nil
	})

	select {
	case res := <-resCh:
		if res.Err != nil {
			return playback.MediaInfo{}, fmt.Errorf("probe failed: %w", res.Err)
		}
		meta := res.Val.(vod.Metadata)

		log.Debug().
			Str("recording_id", recordingID).
			Str("duration_source", "probe").
			Int32("waiters", waiters). // Use captured waiters
			Bool("singleflight_shared", res.Shared).
			Msg("VOD duration resolved via probe")

		return r.mapMediaInfo(meta), nil
	case <-ctx.Done():
		return playback.MediaInfo{}, ctx.Err()
	}
}

func (r *DefaultVODResolver) mapMediaInfo(meta vod.Metadata) playback.MediaInfo {
	return playback.MediaInfo{
		AbsPath:    meta.ResolvedPath,
		Container:  meta.Container,
		VideoCodec: meta.VideoCodec,
		AudioCodec: meta.AudioCodec,
		Duration:   float64(meta.Duration),
	}
}

func (r *DefaultVODResolver) incInterest(key string) (context.Context, int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	act, ok := r.active[key]
	if !ok {
		ictx, cancel := context.WithCancel(r.supervisorCtx)
		act = &vodActivity{
			waiters: 1,
			cancel:  cancel,
			ctx:     ictx,
		}
		r.active[key] = act
	} else {
		act.waiters++
	}
	return act.ctx, act.waiters
}

func (r *DefaultVODResolver) decInterest(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	act, ok := r.active[key]
	if !ok {
		return
	}
	act.waiters--
	if act.waiters == 0 {
		act.cancel()
		delete(r.active, key)
	}
}

func (r *DefaultVODResolver) isWithinNegativeTTL(meta vod.Metadata) bool {
	var ttl time.Duration
	switch meta.FailureKind {
	case vod.FailureTransient:
		ttl = 2 * time.Minute
	case vod.FailureNotFound:
		ttl = 60 * time.Minute
	case vod.FailureCorrupt:
		ttl = 120 * time.Minute
	default:
		return false
	}
	return r.clock.Now().Unix() < meta.FailedAt+int64(ttl.Seconds())
}

func (r *DefaultVODResolver) persistFailure(id string, err error) {
	kind := vod.FailureTransient
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, vod.ErrProbeNotFound) {
		kind = vod.FailureNotFound
	} else if errors.Is(err, vod.ErrProbeCorrupt) {
		kind = vod.FailureCorrupt
	}

	r.vodMgr.UpdateMetadata(id, vod.Metadata{
		State:       vod.ArtifactStateFailed,
		FailureKind: kind,
		FailedAt:    r.clock.Now().Unix(),
		Error:       err.Error(),
	})
}

func (r *DefaultVODResolver) mapFailureKind(kind vod.FailureKind) error {
	switch kind {
	case vod.FailureNotFound:
		return ErrRecordingNotFound // 404
	case vod.FailureCorrupt:
		return fmt.Errorf("media metadata missing or corrupt") // Will map to 422/500
	default:
		return fmt.Errorf("probe temporarily unavailable") // Will map to 503
	}
}

func (r *DefaultVODResolver) decodeID(id string) string {
	id = strings.TrimSpace(id)
	decodedBytes, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(decodedBytes) == 0 || !utf8.Valid(decodedBytes) {
		return ""
	}
	return string(decodedBytes)
}

func (r *DefaultVODResolver) mapResponse(id string, meta vod.Metadata, source string) *types.VODPlaybackResponse {
	playbackType := "hls"
	mime := "application/vnd.apple.mpegurl"
	streamURL := fmt.Sprintf("/api/v3/recordings/%s/playlist.m3u8", id)

	if strings.HasSuffix(strings.ToLower(meta.ResolvedPath), ".mp4") {
		playbackType = "mp4"
		mime = "video/mp4"
		streamURL = fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", id)
	}

	return &types.VODPlaybackResponse{
		RecordingID:     id,
		StreamURL:       streamURL,
		PlaybackType:    playbackType,
		DurationSeconds: int64(meta.Duration),
		MimeType:        mime,
		DurationSource:  source,
	}
}

func (r *DefaultVODResolver) findLibraryItem(ctx context.Context, localPath string) (*library.Item, error) {
	rootID, rel, found := r.matchToRoots(localPath)
	if !found {
		return nil, nil
	}
	return r.libStore.GetItem(ctx, rootID, rel)
}

func (r *DefaultVODResolver) matchToRoots(localPath string) (string, string, bool) {
	localPath = filepath.Clean(localPath)
	var bestRoot *library.RootConfig
	longestPrefix := -1

	for i := range r.roots {
		root := &r.roots[i]
		cleanRoot := filepath.Clean(root.Path)
		if strings.HasPrefix(localPath, cleanRoot) {
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

	return bestRoot.ID, rel, true
}
