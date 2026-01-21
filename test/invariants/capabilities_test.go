package invariants

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvariant5_LegacyAnonEqualsWebConservative(t *testing.T) {
	ctx := context.Background()

	// Act: Legacy (empty profile)
	legacyCaps := recordings.ResolveCapabilities(ctx, "anonymous", "legacy", "", nil, nil)

	// Act: Explicit web_conservative
	webCaps := recordings.ResolveCapabilities(ctx, "anonymous", "v3.1", "web_conservative", nil, nil)

	// Assert: Struct parity
	// Note: reflect.DeepEqual is used as a baseline bit-for-bit check
	legacyCaps = capabilities.CanonicalizeCapabilities(legacyCaps)
	webCaps = capabilities.CanonicalizeCapabilities(webCaps)
	assert.True(t, reflect.DeepEqual(legacyCaps, webCaps), "Legacy anonymous must be bit-for-bit identical to web_conservative")

	// Assert: JSON parity
	assert.JSONEq(t, canonicalJSON(t, legacyCaps), canonicalJSON(t, webCaps))
}

func TestInvariant6_AllProfilesMatchFixtures(t *testing.T) {
	// Profiles to test should match those defined in the generator and resolver
	profiles := []string{"web_conservative", "vlc_desktop", "stb_enigma2", "android_tv", "tvos"}
	ctx := context.Background()

	for _, p := range profiles {
		t.Run(p, func(t *testing.T) {
			// Act
			caps := recordings.ResolveCapabilities(ctx, "anonymous", "v3.1", p, nil, nil)

			// Load Fixture
			fixture := loadFixture(t, p)

			// Assert: Struct parity
			caps = capabilities.CanonicalizeCapabilities(caps)
			fixture = capabilities.CanonicalizeCapabilities(fixture)
			assert.True(t, reflect.DeepEqual(caps, fixture), fmt.Sprintf("Resolver output for %s must match bit-for-bit fixture", p))

			// Assert: JSON parity
			assert.JSONEq(t, canonicalJSON(t, caps), canonicalJSON(t, fixture))
		})
	}
}

func TestV31CapsNoServerExtension(t *testing.T) {
	ctx := context.Background()
	clientCaps := capabilities.PlaybackCapabilities{
		CapabilitiesVersion: 1,
		VideoCodecs:         []string{"h264"},
		AudioCodecs:         []string{"aac"},
	}

	// Act: Resolver with valid Client-Caps
	resolved := recordings.ResolveCapabilities(ctx, "anonymous", "v3.1", "", nil, &clientCaps)

	// Assert: No extension
	assert.ElementsMatch(t, []string{"h264"}, resolved.VideoCodecs)
	assert.ElementsMatch(t, []string{"aac"}, resolved.AudioCodecs)
	assert.False(t, resolved.SupportsHLS)
}

func TestInvariant7_WebConservativeAC3Transcode(t *testing.T) {
	ctx := context.Background()

	// 1. Resolve Capabilities (web_conservative)
	caps := recordings.ResolveCapabilities(ctx, "anonymous", "v3.1", "web_conservative", nil, nil)

	// Ensure AC3 is NOT in web_conservative
	assert.NotContains(t, caps.AudioCodecs, "ac3")

	// 2. Setup Decision Input (Source with AC3)
	input := decision.Input{
		APIVersion: "v3.1",
		Source: decision.Source{
			Container:  "mp4",  // supported
			VideoCodec: "h264", // supported
			AudioCodec: "ac3",  // UNSUPPORTED
			Width:      1920,
			Height:     1080,
		},
		Capabilities: decision.FromCapabilities(caps),
		Policy: decision.Policy{
			AllowTranscode: true,
		},
	}

	// 3. Decide
	_, dec, prob := decision.Decide(ctx, input)
	require.Nil(t, prob)

	// 4. Assert Transcode
	assert.Equal(t, decision.ModeTranscode, dec.Mode, "Must transcode due to AC3 audio")
	assert.Equal(t, "audio_codec_not_supported_by_client", string(dec.Reasons[0]))
}

func TestInvariant8_AllowTranscodeConstraint(t *testing.T) {
	ctx := context.Background()

	// 1. Client prohibits transcoding
	allowTranscode := false
	serverCaps := true // Server CAN transcode (simulated in handler, but here we assume Policy passes through)

	clientCaps := capabilities.PlaybackCapabilities{
		CapabilitiesVersion: 1,
		Containers:          []string{"mp4"},
		VideoCodecs:         []string{"h264"},
		AudioCodecs:         []string{"aac"},
		AllowTranscode:      &allowTranscode,
	}

	// 2. Resolve (v3.1)
	resolved := recordings.ResolveCapabilities(ctx, "anonymous", "v3.1", "", nil, &clientCaps)

	// Assert resolved capabilities honor constraint (copied over)
	assert.NotNil(t, resolved.AllowTranscode)
	assert.False(t, *resolved.AllowTranscode)

	// 3. Setup Decision (Simulating Handler Logic for AllowTranscode composition)
	// HANDLER LOGIC SIMULATION:
	// allowTranscode := serverCanTranscode && clientAllowsTranscode
	finalPolicyAllow := serverCaps && (*resolved.AllowTranscode)

	input := decision.Input{
		APIVersion: "v3.1",
		Source: decision.Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3", // Needs transcode
			Width:      1920,
			Height:     1080,
		},
		Capabilities: decision.FromCapabilities(resolved),
		Policy: decision.Policy{
			AllowTranscode: finalPolicyAllow, // Should be false
		},
	}

	// 4. Decide
	_, dec, prob := decision.Decide(ctx, input)
	require.Nil(t, prob)

	// 5. Assert Deny (Transcoding needed but disallowed)
	assert.Equal(t, decision.ModeDeny, dec.Mode, "Must deny because transcoding is required but disabled by client")
	assert.Contains(t, dec.Reasons, decision.ReasonCode("policy_denies_transcode"), "Reason code mismatch")
}

// Helper to map internal caps to decision caps (duplicated from handler for test isolation)
// function removed (replaced by decision.FromCapabilities)

// Helpers

func loadFixture(t *testing.T, profile string) capabilities.PlaybackCapabilities {
	t.Helper()
	// Find repo root (assuming running from xg2g root or test/invariants)
	// We'll use relative paths if possible
	basePath := "fixtures/capabilities"
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		basePath = "../../fixtures/capabilities" // try parent
	}

	path := filepath.Join(basePath, profile+".json")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "Failed to read fixture: %s", path)

	// Since production struct uses camelCase JSON tags but fixtures use snake_case,
	// we use a temporary mapping struct or simply a generic map to bridge them.
	var fixtureRaw struct {
		CapabilitiesVersion int      `json:"capabilities_version"`
		Containers          []string `json:"containers"`
		VideoCodecs         []string `json:"video_codecs"`
		AudioCodecs         []string `json:"audio_codecs"`
		SupportsHLS         bool     `json:"supports_hls"`
		SupportsRange       *bool    `json:"supports_range"`
	}
	err = json.Unmarshal(raw, &fixtureRaw)
	require.NoError(t, err)

	caps := capabilities.PlaybackCapabilities{
		CapabilitiesVersion: fixtureRaw.CapabilitiesVersion,
		Containers:          fixtureRaw.Containers,
		VideoCodecs:         fixtureRaw.VideoCodecs,
		AudioCodecs:         fixtureRaw.AudioCodecs,
		SupportsHLS:         fixtureRaw.SupportsHLS,
		SupportsRange:       fixtureRaw.SupportsRange,
	}

	return capabilities.CanonicalizeCapabilities(caps)
}

func canonicalJSON(t *testing.T, caps capabilities.PlaybackCapabilities) string {
	t.Helper()
	c := capabilities.CanonicalizeCapabilities(caps)
	b, err := json.MarshalIndent(c, "", "  ")
	require.NoError(t, err)
	return string(b)
}
