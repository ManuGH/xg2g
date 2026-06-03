package ffmpeg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

// TestDecodeVerifyVerdicts is the controlled forced-flip acceptance for the
// three-state verdict: both negative states (withheld, unverifiable) must fire
// on demand, not just the state this host happens to produce.
func TestDecodeVerifyVerdicts(t *testing.T) {
	const want = 10
	cases := []struct {
		name      string
		decoderOK bool
		frames    int
		yavg      float64
		rng       float64
		statsErr  error
		verdict   hardware.EncoderVerdict
		reasonHas string
	}{
		{"verified", true, 10, 100, 80, nil, hardware.VerdictVerified, ""},
		// unverifiable: no software decoder to validate output — fail-closed, NOT withheld.
		{"no_decoder_unverifiable", false, 0, 0, 0, nil, hardware.VerdictUnverifiable, "no software"},
		// withheld: decoder present but decode failed (corrupt bitstream).
		{"decode_error_withheld", true, 0, 0, 0, errors.New("error submitting packet to decoder"), hardware.VerdictWithheld, "decode"},
		// withheld: partial decode (truncated/aborted output).
		{"partial_withheld", true, 5, 100, 80, nil, hardware.VerdictWithheld, "partial"},
		// withheld: black output.
		{"dark_withheld", true, 10, 10, 80, nil, hardware.VerdictWithheld, "dark"},
		// withheld: flat/grayfield output (passes a luma-mean-only check but not variance).
		{"flat_withheld", true, 10, 100, 5, nil, hardware.VerdictWithheld, "flat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Detector{
				softwareDecoderFn: func(string) (string, bool) { return "libdav1d", tc.decoderOK },
				decodeStatsFn: func(context.Context, string, string) (int, float64, float64, error) {
					return tc.frames, tc.yavg, tc.rng, tc.statsErr
				},
			}
			verdict, reason := d.decodeVerifyEncode(context.Background(), "x.mkv", "av1", want)
			if verdict != tc.verdict {
				t.Fatalf("verdict = %q, want %q (reason: %s)", verdict, tc.verdict, reason)
			}
			if tc.reasonHas != "" && !strings.Contains(reason, tc.reasonHas) {
				t.Errorf("reason %q should contain %q", reason, tc.reasonHas)
			}
		})
	}
}

// TestDecodeVerifyYAvgThresholdFlip proves the "withheld" path fires on demand by
// raising the YAVG threshold above any real value — the host-level forced flip.
func TestDecodeVerifyYAvgThresholdFlip(t *testing.T) {
	d := &Detector{
		minDecodeYAvg:     200, // above any real 8-bit luma mean
		softwareDecoderFn: func(string) (string, bool) { return "libdav1d", true },
		decodeStatsFn: func(context.Context, string, string) (int, float64, float64, error) {
			return 10, 120, 80, nil // verified at the default 32 threshold
		},
	}
	verdict, reason := d.decodeVerifyEncode(context.Background(), "x.mkv", "av1", 10)
	if verdict != hardware.VerdictWithheld {
		t.Fatalf("verdict = %q, want withheld (reason: %s)", verdict, reason)
	}
}
