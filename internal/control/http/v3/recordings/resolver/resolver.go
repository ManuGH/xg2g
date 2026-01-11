package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/http/v3/types"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/recordings"
	"golang.org/x/sync/singleflight"
)

var ErrRecordingNotFound = errors.New("recording not found")

type Resolver interface {
	Resolve(ctx context.Context, recordingID string, intent types.PlaybackIntent, profile playback.ClientProfile) (ResolveOK, *ResolveError)
}

type DefaultResolver struct {
	cfg           *config.AppConfig
	vodManager    *vod.Manager
	durationStore DurationStore // Optional: for Duration Truth persistence
	pathResolver  PathResolver  // Optional: for library coordinate mapping
	Probe         func(ctx context.Context, sourceURL string) error
	sf            singleflight.Group
}

func New(cfg *config.AppConfig, manager *vod.Manager) *DefaultResolver {
	return &DefaultResolver{
		cfg:        cfg,
		vodManager: manager,
		Probe:      DefaultProbe, // defined in probe.go
	}
}

// WithDurationStore adds Duration Truth persistence capability.
func (r *DefaultResolver) WithDurationStore(store DurationStore, pathResolver PathResolver) *DefaultResolver {
	r.durationStore = store
	r.pathResolver = pathResolver
	return r
}

// Resolve implements the Resolver interface.
func (r *DefaultResolver) Resolve(ctx context.Context, recordingID string, intent types.PlaybackIntent, profile playback.ClientProfile) (ResolveOK, *ResolveError) {
	// 1. Validate ID
	serviceRef, ok := decodeRecordingID(recordingID)
	if !ok {
		return ResolveOK{}, &ResolveError{Code: CodeInvalid, Detail: "invalid recording id format"}
	}
	// PR4.1: Use Canonical Validator
	if err := v3recordings.ValidateRecordingRef(serviceRef); err != nil {
		return ResolveOK{}, &ResolveError{Code: CodeInvalid, Err: err, Detail: "invalid recording ref"}
	}

	// 2. Resolve Playback Source (Upstream Check) + Library Coordinates
	kind, source, name, err := r.resolveSource(ctx, serviceRef)
	if err != nil {
		if errors.Is(err, ErrRecordingNotFound) {
			return ResolveOK{}, &ResolveError{Code: CodeNotFound, Err: err}
		}
		return ResolveOK{}, &ResolveError{Code: CodeUpstream, Err: err, Detail: "source resolution failed"}
	}

	// 2a. Duration Truth: Check store first (if wired)
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
			// Store lookup
			if dur, ok, _ := r.durationStore.GetDuration(ctx, rootID, relPath); ok && dur > 0 {
				storeDuration = dur
				durationReason = "resolved_via_store"
			}
		}
	}
	if localPath == "" && strings.HasPrefix(source, "file://") {
		localPath = strings.TrimPrefix(source, "file://")
	}

	// 3. Check VOD Build State (PR4.1: Canonical Cache Dir)
	cacheDir, err := v3recordings.RecordingCacheDir(r.cfg.HLS.Root, serviceRef)
	if err != nil {
		return ResolveOK{}, &ResolveError{Code: CodeInternal, Err: err, Detail: "cache dir calc failed"}
	}

	if status, exists := r.vodManager.Get(ctx, cacheDir); exists {
		if status.State == vod.JobStateBuilding || status.State == vod.JobStateFinalizing {
			return ResolveOK{}, &ResolveError{
				Code:       CodePreparing,
				RetryAfter: 5 * time.Second,
				Detail:     "transcoding in progress",
			}
		}
	}

	// 4. Persistence/Metadata Sync
	meta, metaOk := r.vodManager.GetMetadata(serviceRef)

	needsProbe := storeDuration == 0 && (!metaOk || meta.Duration <= 0)
	if needsProbe {
		// Policy: Blocking probe with budget and Singleflight
		// PR4.2: Key Hygiene (Hash to prevent leak of credentials if in source)
		// We key on kind + source (potentially containing auth)
		// Hash it to be safe.
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

			if err := r.Probe(probeCtx, source); err != nil {
				return nil, err
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
			// Handle Timeout explictly as PREPARING (503) without forbidden string matching
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return ResolveOK{}, &ResolveError{
					Code:       CodePreparing,
					RetryAfter: 2 * time.Second,
					Detail:     "media analysis in progress",
				}
			}
			if errors.Is(err, vod.ErrProbeNotFound) {
				return ResolveOK{}, &ResolveError{Code: CodeNotFound, Err: err}
			}
			// If upstream is 404/500, map to Upstream/NotFound
			return ResolveOK{}, &ResolveError{Code: CodeUpstream, Err: err, Detail: "source probe failed"}
		}

		meta = val.(vod.Metadata)
		if meta.ResolvedPath == "" {
			meta.ResolvedPath = localPath
		}
		r.vodManager.UpdateMetadata(serviceRef, meta)

		if storeDuration == 0 && r.durationStore != nil && rootID != "" && relPath != "" && meta.Duration > 0 {
			if err := r.durationStore.SetDuration(ctx, rootID, relPath, meta.Duration); err != nil {
				return ResolveOK{}, &ResolveError{Code: CodeInternal, Err: err, Detail: "duration persistence failed"}
			}
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

	// Convert vod.Metadata -> playback.MediaInfo (PR4.1 Contract Fix)
	// Use store duration if available (Duration Truth)
	finalDuration := float64(meta.Duration)
	if storeDuration > 0 {
		finalDuration = float64(storeDuration)
	}
	if durationReason == "" {
		durationReason = "resolved_via_metadata" // from vodManager
	}

	info := playback.MediaInfo{
		AbsPath:    source,
		Container:  meta.Container,
		VideoCodec: meta.VideoCodec,
		AudioCodec: meta.AudioCodec,
		Duration:   finalDuration,
	}

	// 5. Decision Engine
	decision, err := playback.Decide(profile, info, playback.Policy{})
	if err != nil {
		return ResolveOK{}, &ResolveError{Code: CodeInternal, Err: err, Detail: "decision engine error"}
	}

	// 6. Enforce Decision (HLS persistence)
	if decision.Mode == playback.ModeTranscode && decision.Artifact == playback.ArtifactHLS {
		playlistName := "index.m3u8"
		if kind == "receiver" {
			playlistName = "index.live.m3u8"
		}
		finalPath := filepath.Join(cacheDir, playlistName)

		// Explicit Policy: Resolver causes persistence side effect
		_, err := r.vodManager.EnsureSpec(ctx, cacheDir, serviceRef, source, cacheDir, name, finalPath, vod.ProfileDefault)
		if err != nil {
			return ResolveOK{}, &ResolveError{Code: CodeFailed, Err: err, Detail: "failed to queue transcoding"}
		}
	}

	return ResolveOK{
		Decision:  decision,
		MediaInfo: info,
		Reason:    durationReason,
	}, nil
}

// resolveSource determines local file vs upstream stream
func (r *DefaultResolver) resolveSource(ctx context.Context, serviceRef string) (kind, source, name string, err error) {
	// PR4.2: Correctly extract path for local mapping
	// serviceRef for Enigma2 is "1:0:..."
	// recordings.ExtractPathFromServiceRef handles finding the path component at the end
	receiverPath := recordings.ExtractPathFromServiceRef(serviceRef)

	policy := r.cfg.RecordingPlaybackPolicy
	allowLocal := policy != config.PlaybackPolicyReceiverOnly
	allowReceiver := policy != config.PlaybackPolicyLocalOnly

	if allowLocal {
		mapper := recordings.NewPathMapper(r.cfg.RecordingPathMappings)
		if localPath, ok := mapper.ResolveLocalExisting(receiverPath); ok {
			return "local", "file://" + localPath, filepath.Base(localPath), nil
		}
	}

	if allowReceiver {
		// Resolve Receiver URL with Canonical Escaping
		baseURL, err := url.Parse(r.cfg.Enigma2.BaseURL)
		if err != nil {
			return "", "", "", fmt.Errorf("invalid enigma2 base url: %w", err)
		}

		u := *baseURL
		u.Host = fmt.Sprintf("%s:%d", baseURL.Hostname(), r.cfg.Enigma2.StreamPort)
		if r.cfg.Enigma2.Username != "" {
			u.User = url.UserPassword(r.cfg.Enigma2.Username, r.cfg.Enigma2.Password)
		}

		// PR4.2: Correctly set Escaped Path
		// Set Path to the raw string (for clarity/std lib)
		u.Path = "/" + serviceRef

		// Set RawPath to the strictly escaped version
		// EscapeServiceRefPath performs percentage encoding of special chars in ref
		u.RawPath = "/" + v3recordings.EscapeServiceRefPath(serviceRef)

		return "receiver", u.String(), "", nil
	}

	return "", "", "", ErrRecordingNotFound
}

// --- Local Helpers ---

func hashSingleflightKey(kind, source string) string {
	// sha256 to allow safe logging/usage
	sum := sha256.Sum256([]byte(kind + "|" + source))
	return hex.EncodeToString(sum[:])
}

func decodeRecordingID(id string) (string, bool) {
	// Try Raw URL Encoding first (no padding)
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err == nil {
		return string(b), true
	}
	// Fallback to Raw StdEncoding
	b, err = base64.RawStdEncoding.DecodeString(id)
	if err == nil {
		return string(b), true
	}
	return "", false
}
