package recordings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	platformpaths "github.com/ManuGH/xg2g/internal/platform/paths"
)

// This file is the single source of truth for recording build identity.
// Any new HLS/status/build path must derive cache keys, metadata keys, and
// canonical target variants from these helpers instead of open-coding them.

// RecordingCacheDir returns the canonical directory for the default materialized recording build.
func RecordingCacheDir(hlsRoot, serviceRef string) (string, error) {
	return RecordingVariantCacheDir(hlsRoot, serviceRef, "")
}

// RecordingVariantCacheDir returns the canonical directory for a materialized recording build variant.
func RecordingVariantCacheDir(hlsRoot, serviceRef, variant string) (string, error) {
	if strings.TrimSpace(hlsRoot) == "" {
		return "", fmt.Errorf("hls root not configured")
	}
	return platformpaths.RecordingArtifactDir(hlsRoot, RecordingVariantCacheKey(serviceRef, variant)), nil
}

// RecordingCacheKey returns the stable key for the default recording build.
func RecordingCacheKey(serviceRef string) string {
	return RecordingVariantCacheKey(serviceRef, "")
}

// RecordingVariantCacheKey returns the stable key for a recording build variant.
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

// NormalizeVariantHash normalizes the query-facing variant token.
func NormalizeVariantHash(variant string) string {
	return normalize.Token(variant)
}

// RecordingVariantMetadataKey returns the canonical metadata cache key for a build variant.
func RecordingVariantMetadataKey(serviceRef, variant string) string {
	serviceRef = strings.TrimSpace(serviceRef)
	variant = NormalizeVariantHash(variant)
	if variant == "" {
		return serviceRef
	}
	return serviceRef + "#variant:" + variant
}

// RecordingTargetVariantHash returns the canonical variant hash for a concrete target profile.
func RecordingTargetVariantHash(target *playbackprofile.TargetPlaybackProfile) string {
	if target == nil {
		return ""
	}
	canonical := playbackprofile.CanonicalizeTarget(*target)
	return canonical.Hash()
}

// DefaultRecordingVariantHash returns the canonical variant used by the default recordings HLS route.
func DefaultRecordingVariantHash() string {
	return RecordingTargetVariantHash(RecordingTargetProfile(""))
}

// RecordingTargetProfile resolves a public recordings profile into the canonical HLS build target.
func RecordingTargetProfile(profile string) *playbackprofile.TargetPlaybackProfile {
	raw := normalize.Token(profile)
	publicProfile := profiles.PublicProfileName(raw)
	if raw == "android_native" || raw == "android_tv_native" {
		publicProfile = profiles.PublicProfileCompatible
	}
	packaging, segmentContainer, container := resolveRecordingPackaging(raw)

	target := playbackprofile.TargetPlaybackProfile{
		Container: container,
		Packaging: packaging,
		Video:     recordingVideoTarget(publicProfile),
		Audio:     recordingAudioTarget(publicProfile),
		HLS: playbackprofile.HLSTarget{
			Enabled:          true,
			SegmentContainer: segmentContainer,
			SegmentSeconds:   6,
		},
		HWAccel: playbackprofile.HWAccelNone,
	}
	canonical := playbackprofile.CanonicalizeTarget(target)
	return &canonical
}

func resolveRecordingPackaging(raw string) (playbackprofile.Packaging, string, string) {
	switch raw {
	case "android_native", "android_tv_native", "safari", "safari_dvr", "safari_dirty", "safari_hevc", "safari_hevc_hw", "safari_hevc_hw_ll", "h264_fmp4":
		return playbackprofile.PackagingFMP4, "fmp4", "mp4"
	default:
		return playbackprofile.PackagingTS, "mpegts", "mpegts"
	}
}

func recordingVideoTarget(publicProfile string) playbackprofile.VideoTarget {
	rung := recordingVideoQualityRung(publicProfile)
	if rung == playbackprofile.RungUnknown {
		return playbackprofile.VideoTarget{
			Mode: playbackprofile.MediaModeCopy,
		}
	}
	return playbackprofile.VideoTarget{
		Mode:   playbackprofile.MediaModeTranscode,
		Codec:  "h264",
		CRF:    playbackprofile.VideoCRFForRung(rung),
		Preset: playbackprofile.VideoPresetForRung(rung),
	}
}

func recordingAudioTarget(publicProfile string) playbackprofile.AudioTarget {
	target := playbackprofile.AudioTarget{
		Mode:       playbackprofile.MediaModeTranscode,
		Codec:      "aac",
		Channels:   2,
		SampleRate: 48000,
	}
	switch publicProfile {
	case string(playbackprofile.IntentQuality):
		target.BitrateKbps = 320
	case string(playbackprofile.IntentRepair):
		target.BitrateKbps = 192
	case string(playbackprofile.IntentDirect):
		target.Mode = playbackprofile.MediaModeCopy
	default:
		target.BitrateKbps = 256
	}
	return target
}

func recordingVideoQualityRung(publicProfile string) playbackprofile.QualityRung {
	switch publicProfile {
	case string(playbackprofile.IntentQuality):
		return playbackprofile.RungQualityVideoH264CRF20
	case string(playbackprofile.IntentRepair):
		return playbackprofile.RungRepairVideoH264CRF28
	case string(playbackprofile.IntentCompatible):
		return playbackprofile.RungCompatibleVideoH264CRF23
	default:
		return playbackprofile.RungUnknown
	}
}
