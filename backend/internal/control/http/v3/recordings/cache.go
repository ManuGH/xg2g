package recordings

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/normalize"
)

// RecordingCacheDir returns the canonical directory for a recording's assets.
// It follows the layout: <hlsRoot>/recordings/<sha256(serviceRef)>
func RecordingCacheDir(hlsRoot, serviceRef string) (string, error) {
	return recservice.RecordingCacheDir(hlsRoot, serviceRef)
}

// RecordingVariantCacheDir returns the canonical directory for a recording variant's assets.
// It follows the layout: <hlsRoot>/recordings/<sha256(serviceRef|variant)>.
func RecordingVariantCacheDir(hlsRoot, serviceRef, variant string) (string, error) {
	return recservice.RecordingVariantCacheDir(hlsRoot, serviceRef, variant)
}

// RecordingCacheKey returns the stable hash key for a serviceRef.
func RecordingCacheKey(serviceRef string) string {
	return recservice.RecordingCacheKey(serviceRef)
}

// RecordingVariantCacheKey returns the stable hash key for a serviceRef plus concrete playback variant.
func RecordingVariantCacheKey(serviceRef, variant string) string {
	return recservice.RecordingVariantCacheKey(serviceRef, variant)
}

// NormalizeVariantHash normalizes the query-facing variant hash token.
func NormalizeVariantHash(variant string) string {
	return recservice.NormalizeVariantHash(variant)
}

// TargetVariantHash returns the stable variant hash for a target playback profile.
func TargetVariantHash(target *playbackprofile.TargetPlaybackProfile) string {
	return recservice.RecordingTargetVariantHash(target)
}

// EncodeTargetProfileQuery encodes a canonical target playback profile for URL transport with an HMAC signature.
func EncodeTargetProfileQuery(intent *ports.BuildIntent, signingKey string) (string, error) {
	if intent == nil {
		return "", fmt.Errorf("intent cannot be nil")
	}

	intentCopy := *intent
	intentCopy.Target = playbackprofile.CanonicalizeTarget(intentCopy.Target)
	if intentCopy.IntentHash == "" {
		intentCopy.IntentHash = TargetVariantHash(&intentCopy.Target)
	}
	// Do NOT canonicalize the SourceProfile, we pass it exactly as probed.

	b, err := json.Marshal(&intentCopy)
	if err != nil {
		return "", fmt.Errorf("marshal intent: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(b)

	if signingKey == "" {
		return payload, nil
	}

	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payload + "." + signature, nil
}

// DecodeTargetProfileQuery decodes a target playback profile from the URL-safe query representation and verifies its HMAC signature.
func DecodeTargetProfileQuery(raw, primaryKey, previousKey string, strictTargetRequired bool) (*ports.BuildIntent, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ".")
	if len(parts) > 2 {
		return nil, fmt.Errorf("invalid target profile format")
	}

	payload := parts[0]
	var signature string
	if len(parts) == 2 {
		signature = parts[1]
	}

	if signature == "" {
		if strictTargetRequired {
			return nil, fmt.Errorf("missing target profile signature (strict mode)")
		}
		log.L().Warn().Msg("unsigned target profile received; accepting in legacy mode")
		metrics.IncTargetFallback("unsigned")
	} else {
		valid := false
		if checkHMAC(payload, signature, primaryKey) {
			valid = true
		} else if previousKey != "" && checkHMAC(payload, signature, previousKey) {
			valid = true
		}

		if !valid {
			return nil, fmt.Errorf("invalid target profile signature")
		}
	}

	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode target profile: %w", err)
	}
	var intent struct {
		Target      *playbackprofile.TargetPlaybackProfile `json:"target"`
		SourceTruth *ports.SourceProfile                   `json:"sourceTruth"`
	}

	if err := json.Unmarshal(b, &intent); err == nil && intent.Target != nil {
		canonical := playbackprofile.CanonicalizeTarget(*intent.Target)
		source := ports.SourceProfile{}
		if intent.SourceTruth != nil {
			source = *intent.SourceTruth
		}
		return &ports.BuildIntent{
			IntentHash:  TargetVariantHash(&canonical),
			SourceTruth: source,
			Target:      canonical,
		}, nil
	}

	// TODO(SPEC_MODERNIZATION_2026 §R0): Remove legacy bare TargetPlaybackProfile fallback after a full deploy cycle.
	// Legacy unsigned/nackte Target-JSON fallback
	var target playbackprofile.TargetPlaybackProfile
	if err := json.Unmarshal(b, &target); err != nil {
		return nil, fmt.Errorf("unmarshal target profile: %w", err)
	}
	canonical := playbackprofile.CanonicalizeTarget(target)
	return &ports.BuildIntent{
		IntentHash: TargetVariantHash(&canonical),
		Target:     canonical,
	}, nil
}

func checkHMAC(payload, signature, key string) bool {
	if key == "" {
		return false
	}
	expectedMAC := hmac.New(sha256.New, []byte(key))
	expectedMAC.Write([]byte(payload))

	decodedSig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}

	return hmac.Equal(decodedSig, expectedMAC.Sum(nil))
}

// RecordingPlaylistURL returns the variant-aware playlist URL for a recording.
func RecordingPlaylistURL(recordingID, profile string, intent *ports.BuildIntent, signingKey string) string {
	base := fmt.Sprintf("/api/v3/recordings/%s/playlist.m3u8", recordingID)
	params := make([]string, 0, 3)
	if p := normalize.Token(profile); p != "" {
		params = append(params, "profile="+p)
	}
	if intent != nil {
		variant := TargetVariantHash(&intent.Target)
		if variant != "" {
			params = append(params, "variant="+variant)
			if encoded, err := EncodeTargetProfileQuery(intent, signingKey); err == nil && encoded != "" {
				params = append(params, "target="+encoded)
			}
		}
	}
	if len(params) == 0 {
		return base
	}
	return base + "?" + strings.Join(params, "&")
}
