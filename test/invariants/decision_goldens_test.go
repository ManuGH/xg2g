package invariants

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GoldenFixture reflects the strict JSON schema for decision goldens.
type GoldenFixture struct {
	Name     string            `json:"name"`
	Input    GoldenInput       `json:"input"`
	Expected GoldenEcpectation `json:"expected"`
}

type GoldenInput struct {
	Truth        GoldenSource       `json:"truth"`
	Capabilities GoldenCapabilities `json:"capabilities"`
	Policy       GoldenPolicy       `json:"policy"`
}

type GoldenSource struct {
	Container   string  `json:"container"`
	VideoCodec  string  `json:"video_codec"`
	AudioCodec  string  `json:"audio_codec"`
	BitrateKbps int     `json:"bitrate_kbps"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	FPS         float64 `json:"fps"`
}

type GoldenPolicy struct {
	AllowTranscode bool `json:"allow_transcode"`
}

type GoldenCapabilities struct {
	Version       int      `json:"version"`
	Containers    []string `json:"containers"`
	VideoCodecs   []string `json:"video_codecs"`
	AudioCodecs   []string `json:"audio_codecs"`
	SupportsHLS   bool     `json:"supports_hls"`
	SupportsRange *bool    `json:"supports_range"`
}

type GoldenEcpectation struct {
	IsProblem bool `json:"is_problem"`
	Problem   *struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	} `json:"problem"`
	Mode     decision.Mode         `json:"mode"`
	Protocol string                `json:"protocol"`
	Reasons  []decision.ReasonCode `json:"reasons"`
}

func TestDecisionGoldens(t *testing.T) {
	// 1. Locate Fixtures
	fixtureDir := "../../fixtures/decision" // Assuming running from test/invariants
	files, err := filepath.Glob(filepath.Join(fixtureDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "No golden fixtures found in %s", fixtureDir)

	ctx := context.Background()

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			// 2. Load Fixture
			content, err := os.ReadFile(file)
			require.NoError(t, err)

			var golden GoldenFixture
			err = json.Unmarshal(content, &golden)
			require.NoError(t, err, "Failed to unmarshal golden fixture")

			// 3. Map to Internal Input
			input := decision.Input{
				Source: decision.Source{
					Container:   golden.Input.Truth.Container,
					VideoCodec:  golden.Input.Truth.VideoCodec,
					AudioCodec:  golden.Input.Truth.AudioCodec,
					BitrateKbps: golden.Input.Truth.BitrateKbps,
					Width:       golden.Input.Truth.Width,
					Height:      golden.Input.Truth.Height,
					FPS:         golden.Input.Truth.FPS,
				},
				Capabilities: decision.Capabilities{
					Version:       golden.Input.Capabilities.Version,
					Containers:    golden.Input.Capabilities.Containers,
					VideoCodecs:   golden.Input.Capabilities.VideoCodecs,
					AudioCodecs:   golden.Input.Capabilities.AudioCodecs,
					SupportsHLS:   golden.Input.Capabilities.SupportsHLS,
					SupportsRange: golden.Input.Capabilities.SupportsRange,
					// MaxVideo & DeviceType optional/not in goldens yet
				},
				Policy: decision.Policy{
					AllowTranscode: golden.Input.Policy.AllowTranscode,
				},
				APIVersion: "p8-frozen", // Virtual API context
				RequestID:  "golden-test-request",
			}

			// 4. Run Decision Engine (Black Box)
			_, dec, prob := decision.Decide(ctx, input)

			// 5. Assertions
			if golden.Expected.IsProblem {
				require.NotNil(t, prob, "Expected problem response, got nil")
				require.Nil(t, dec, "Expected nil decision when problem occurs")
				if golden.Expected.Problem != nil {
					assert.Equal(t, golden.Expected.Problem.Status, prob.Status)
					assert.Equal(t, golden.Expected.Problem.Code, prob.Code)
				}
				return // End test for problem case
			}

			// Must be valid decision (prob nil) unless expected?
			// Goldens define Output (Mode/Reasons), so usually implied 200 unless 422?
			// If expected mode is empty/missing? Goldens have expected block.
			require.Nil(t, prob, "Decision engine returned problem: %v", prob)
			require.NotNil(t, dec, "Decision engine returned nil decision")

			// Mode
			assert.Equal(t, golden.Expected.Mode, dec.Mode, "Mode mismatch")

			// Reasons (Order Matters!)
			assert.Equal(t, golden.Expected.Reasons, dec.Reasons, "Reasons mismatch (Order/Codes)")

			// Protocol (Using Single Mapping Truth)
			protocol := decision.ProtocolFrom(dec)
			assert.Equal(t, golden.Expected.Protocol, protocol, "Protocol mismatch")
		})
	}
}
