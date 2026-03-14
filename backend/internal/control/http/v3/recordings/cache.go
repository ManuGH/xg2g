package recordings

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

// RecordingCacheDir returns the canonical directory for a recording's assets.
// It follows the layout: <hlsRoot>/recordings/<sha256(serviceRef)>
func RecordingCacheDir(hlsRoot, serviceRef string) (string, error) {
	return RecordingVariantCacheDir(hlsRoot, serviceRef, "")
}

// RecordingVariantCacheDir returns the canonical directory for a recording variant's assets.
// It follows the layout: <hlsRoot>/recordings/<sha256(serviceRef|variant)>.
func RecordingVariantCacheDir(hlsRoot, serviceRef, variant string) (string, error) {
	if strings.TrimSpace(hlsRoot) == "" {
		return "", fmt.Errorf("hls root not configured")
	}
	return filepath.Join(hlsRoot, "recordings", RecordingVariantCacheKey(serviceRef, variant)), nil
}

// RecordingCacheKey returns the stable hash key for a serviceRef.
func RecordingCacheKey(serviceRef string) string {
	return RecordingVariantCacheKey(serviceRef, "")
}

// RecordingVariantCacheKey returns the stable hash key for a serviceRef plus concrete playback variant.
func RecordingVariantCacheKey(serviceRef, variant string) string {
	serviceRef = strings.TrimSpace(serviceRef)
	variant = NormalizeVariantHash(variant)
	if variant == "" {
		sum := sha256.Sum256([]byte(serviceRef))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256([]byte(serviceRef + "\x1f" + variant))
	return hex.EncodeToString(sum[:])
}

// NormalizeVariantHash normalizes the query-facing variant hash token.
func NormalizeVariantHash(variant string) string {
	return normalize.Token(variant)
}

// TargetVariantHash returns the stable variant hash for a target playback profile.
func TargetVariantHash(target *playbackprofile.TargetPlaybackProfile) string {
	if target == nil {
		return ""
	}
	canonical := playbackprofile.CanonicalizeTarget(*target)
	return canonical.Hash()
}

// EncodeTargetProfileQuery encodes a canonical target playback profile for URL transport.
func EncodeTargetProfileQuery(target *playbackprofile.TargetPlaybackProfile) (string, error) {
	if target == nil {
		return "", nil
	}
	canonical := playbackprofile.CanonicalizeTarget(*target)
	b, err := canonical.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DecodeTargetProfileQuery decodes a target playback profile from the URL-safe query representation.
func DecodeTargetProfileQuery(raw string) (*playbackprofile.TargetPlaybackProfile, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode target profile: %w", err)
	}
	var target playbackprofile.TargetPlaybackProfile
	if err := json.Unmarshal(b, &target); err != nil {
		return nil, fmt.Errorf("unmarshal target profile: %w", err)
	}
	canonical := playbackprofile.CanonicalizeTarget(target)
	return &canonical, nil
}

// RecordingPlaylistURL returns the variant-aware playlist URL for a recording.
func RecordingPlaylistURL(recordingID, profile string, target *playbackprofile.TargetPlaybackProfile) string {
	base := fmt.Sprintf("/api/v3/recordings/%s/playlist.m3u8", recordingID)
	params := make([]string, 0, 3)
	if p := normalize.Token(profile); p != "" {
		params = append(params, "profile="+p)
	}
	if variant := TargetVariantHash(target); variant != "" {
		params = append(params, "variant="+variant)
		if encoded, err := EncodeTargetProfileQuery(target); err == nil && encoded != "" {
			params = append(params, "target="+encoded)
		}
	}
	if len(params) == 0 {
		return base
	}
	return base + "?" + strings.Join(params, "&")
}
