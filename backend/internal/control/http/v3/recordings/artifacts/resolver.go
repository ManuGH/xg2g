package artifacts

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/platform/fs"
	"github.com/ManuGH/xg2g/internal/recordings"
)

type Resolver interface {
	ResolvePlaylist(ctx context.Context, recordingID, profile string) (ArtifactOK, *ArtifactError)
	ResolveTimeshift(ctx context.Context, recordingID, profile string) (ArtifactOK, *ArtifactError)
	ResolveSegment(ctx context.Context, recordingID string, segment string) (ArtifactOK, *ArtifactError)
}

type DefaultResolver struct {
	cfg        *config.AppConfig
	vodManager *vod.Manager
	pathMapper *recordings.PathMapper
}

func New(cfg *config.AppConfig, manager *vod.Manager, mapper *recordings.PathMapper) *DefaultResolver {
	return &DefaultResolver{
		cfg:        cfg,
		vodManager: manager,
		pathMapper: mapper,
	}
}

// ResolvePlaylist resolves the HLS playlist (index.m3u8), triggering builds if needed.
func (r *DefaultResolver) ResolvePlaylist(ctx context.Context, recordingID, profile string) (ArtifactOK, *ArtifactError) {
	// 1. Validate ID
	ref, ok := decodeRef(recordingID)
	if !ok {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Detail: "invalid recording id"}
	}

	// Canonical Validation
	if err := v3recordings.ValidateRecordingRef(ref); err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Err: err, Detail: "invalid recording ref"}
	}

	// 2. State Check
	meta, exists := r.vodManager.GetMetadata(ref)
	if !exists {
		// First access: Start pipeline via EnsureSpec
		if err := r.triggerBuild(ctx, ref, profile); err != nil {
			// If build trigger fails (e.g. source not found), map to correct error.
			r.vodManager.TriggerProbe(ref, "build trigger failed: "+err.Error())
		}
		// Return PREPARING to indicate process started
		return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 5 * time.Second, Detail: "preparing"}
	}

	// 3. FAILED Handling (Self-heal)
	if meta.State == vod.ArtifactStateFailed {
		if updated, ok := r.vodManager.PromoteFailedToReadyIfPlaylist(ref); ok {
			meta = updated
		}
	}
	if meta.State == vod.ArtifactStateFailed {
		// Attempt reconcile
		if _, transitioned := r.vodManager.MarkPreparingIfState(ref, vod.ArtifactStateFailed, "reconcile: retrying build"); transitioned {
			_ = r.triggerBuild(ctx, ref, profile)
			return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 5 * time.Second, Detail: "preparing (reconciling)"}
		}

		return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 5 * time.Second, Detail: "preparing (failed state)"}
	}

	// 4. NOT READY
	if meta.State != vod.ArtifactStateReady {
		return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 5 * time.Second, Detail: string(meta.State)}
	}

	// 5. READY
	playlistPath := meta.PlaylistPath
	if playlistPath == "" {
		// Metadata is ready, but we lack an HLS artifact.
		// Trigger/Resume build instead of re-probing to avoid stuck StatePreparing loops.
		_ = r.triggerBuild(ctx, ref, profile)
		return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 2 * time.Second, Detail: "playlist building"}
	}

	// Open and Read for Rewrite
	// #nosec G304 - playlistPath is trusted from internal metadata
	f, err := os.Open(playlistPath)
	if err != nil {
		r.vodManager.DemoteOnOpenFailure(ref, err)
		// Trigger build immediately to recover
		_ = r.triggerBuild(ctx, ref, profile)
		return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 2 * time.Second, Detail: "playlist open failed (reconciling)"}
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInternal, Err: err, Detail: "stat failed"}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInternal, Err: err, Detail: "read failed"}
	}

	// Rewrite using canonical helper
	rewritten := v3recordings.RewritePlaylistType(string(data), "VOD")

	return ArtifactOK{
		Data:    []byte(rewritten),
		ModTime: info.ModTime(),
		Kind:    ArtifactKindPlaylist,
	}, nil
}

func (r *DefaultResolver) ResolveTimeshift(ctx context.Context, recordingID, profile string) (ArtifactOK, *ArtifactError) {
	ref, ok := decodeRef(recordingID)
	if !ok {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Detail: "invalid recording id"}
	}

	if err := v3recordings.ValidateRecordingRef(ref); err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Err: err, Detail: "invalid recording ref"}
	}

	meta, exists := r.vodManager.GetMetadata(ref)
	if !exists || meta.State != vod.ArtifactStateReady {
		// Timeshift piggybacks on VOD build.
		if !exists || meta.State == vod.ArtifactStateFailed {
			_ = r.triggerBuild(ctx, ref, profile)
		}
		return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 2 * time.Second, Detail: "preparing"}
	}

	playlistPath := meta.PlaylistPath
	// #nosec G304 - playlistPath is trusted from internal metadata
	f, err := os.Open(playlistPath)
	if err != nil {
		r.vodManager.DemoteOnOpenFailure(ref, err)
		return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 2 * time.Second}
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInternal, Err: err}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInternal, Err: err}
	}

	// Rewrite using canonical helper
	rewritten := v3recordings.RewritePlaylistType(string(data), "EVENT")
	return ArtifactOK{
		Data:    []byte(rewritten),
		ModTime: info.ModTime(),
		Kind:    ArtifactKindPlaylist,
	}, nil
}

func (r *DefaultResolver) ResolveSegment(ctx context.Context, recordingID string, segment string) (ArtifactOK, *ArtifactError) {
	ref, ok := decodeRef(recordingID)
	if !ok {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Detail: "invalid recording id"}
	}

	if err := v3recordings.ValidateRecordingRef(ref); err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Err: err}
	}

	if !v3recordings.IsAllowedVideoSegment(segment) {
		// Use CodeNotFound with generic detail to avoid revealing policy,
		// or use CodeInvalid/Forbidden if we want to be explicit.
		// User requested: "If you want correctness: add CodeForbidden... or 404 generic".
		// We'll use CodeNotFound + generic message to be safe and consistent with "file not found".
		return ArtifactOK{}, &ArtifactError{Code: CodeNotFound, Detail: "segment not found"}
	}

	cacheDir, err := v3recordings.RecordingCacheDir(r.cfg.HLS.Root, ref)
	if err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInternal, Err: err}
	}

	// Canonical Confinement
	cleanPath, err := fs.ConfineRelPath(cacheDir, segment)
	if err != nil {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Detail: "path traversal prohibited"}
	}
	// Extra safety: reject backslashes if standard confinement doesn't (fs.ConfineRelPath usually does)
	if strings.Contains(segment, "\\") {
		return ArtifactOK{}, &ArtifactError{Code: CodeInvalid, Detail: "invalid path"}
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			if status, ok := r.vodManager.Get(ctx, cacheDir); ok {
				if status.State == vod.JobStateBuilding || status.State == vod.JobStateFinalizing {
					return ArtifactOK{}, &ArtifactError{Code: CodePreparing, RetryAfter: 1 * time.Second, Detail: "segment building"}
				}
			}
			return ArtifactOK{}, &ArtifactError{Code: CodeNotFound, Detail: "segment not found"}
		}
		return ArtifactOK{}, &ArtifactError{Code: CodeInternal, Err: err}
	}

	// Canonical Kind Mapping
	kind := ArtifactKindSegmentTS
	if segment == "init.mp4" {
		kind = ArtifactKindSegmentInit
	} else if strings.HasSuffix(segment, ".m4s") || strings.HasSuffix(segment, ".mp4") {
		kind = ArtifactKindSegmentFMP4
	}

	return ArtifactOK{
		AbsPath: cleanPath,
		ModTime: info.ModTime(),
		Kind:    kind,
	}, nil
}

// Internal Logic

func (r *DefaultResolver) triggerBuild(ctx context.Context, ref, profile string) error {
	// 1. Resolve Source
	srcType, srcURL, _, err := r.resolveSource(ref)
	if err != nil {
		return err
	}

	// 2. Determine Cache Dir
	cacheDir, err := v3recordings.RecordingCacheDir(r.cfg.HLS.Root, ref)
	if err != nil {
		return err
	}

	// 3. Ensure Spec
	finalPath := filepath.Join(cacheDir, "index.m3u8")

	buildProfile := vod.ProfileDefault
	if profile == "safari" {
		buildProfile = vod.ProfileLow
	}

	// Using EnsureSpec to start/resume build
	_, err = r.vodManager.EnsureSpec(ctx, cacheDir, ref, srcURL, cacheDir, "index.live.m3u8", finalPath, buildProfile)
	_ = srcType // Unused for now
	return err
}

func (r *DefaultResolver) resolveSource(serviceRef string) (string, string, string, error) {
	// PR4.2 Invariant: Extract Path
	receiverPath := recordings.ExtractPathFromServiceRef(serviceRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return "", "", "", errors.New("invalid recording path")
	}

	streamPort := r.cfg.Enigma2.StreamPort
	policy := strings.ToLower(strings.TrimSpace(r.cfg.RecordingPlaybackPolicy))
	username := r.cfg.Enigma2.Username
	password := r.cfg.Enigma2.Password

	allowLocal := policy != config.PlaybackPolicyReceiverOnly
	allowReceiver := policy != config.PlaybackPolicyLocalOnly

	if allowLocal && r.pathMapper != nil {
		if localPath, ok := r.pathMapper.ResolveLocalUnsafe(receiverPath); ok {
			// Invariant: Return valid file:// URI structure
			// User requested: "Baue file-URL über url.URL{Scheme:"file", Path: localPath}.String()"
			u := url.URL{
				Scheme: "file",
				Path:   localPath,
			}
			return "local", u.String(), "", nil
		}
	}

	if !allowReceiver {
		return "", "", "", errors.New("recording not found (policy restricted)")
	}

	// PR4.2 Invariant: Canonical URL Escaping
	// We use baseURL parsing logic or manual construction that respects encoded chars.
	// Fix: Parse BaseURL first to preserve scheme/host/userinfo
	baseURL, err := url.Parse(r.cfg.Enigma2.BaseURL)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid config base url: %w", err)
	}

	u := *baseURL
	u.Host = fmt.Sprintf("%s:%d", baseURL.Hostname(), streamPort)

	// RawPath MUST be set to preserved proper escaping of special chars (e.g. %)
	// Path must be unescaped.
	u.Path = "/" + serviceRef // e.g. /1:0:1.../path%20with%20spaces
	// But serviceRef from ExtractPathFromServiceRef might strictly be the ID part?
	// The serviceRef passed here is the full ID string (decoded).
	// We need to escape it for RawPath.
	u.RawPath = "/" + v3recordings.EscapeServiceRefPath(serviceRef)

	if username != "" && password != "" {
		u.User = url.UserPassword(username, password)
	}

	return "receiver", u.String(), "", nil
}

func decodeRef(id string) (string, bool) {
	// Try Hex (Priority)
	if b, err := hex.DecodeString(id); err == nil {
		return string(b), true
	}
	// Try RawURL (No Padding)
	if b, err := base64.RawURLEncoding.DecodeString(id); err == nil {
		return string(b), true
	}
	// Try URL Encoding (Padding)
	if b, err := base64.URLEncoding.DecodeString(id); err == nil {
		return string(b), true
	}
	// Try RawStd (No Padding)
	if b, err := base64.RawStdEncoding.DecodeString(id); err == nil {
		return string(b), true
	}
	// Fallback to StdEncoding (Padding)
	if b, err := base64.StdEncoding.DecodeString(id); err == nil {
		return string(b), true
	}
	return "", false
}
