package ffmpeg

import (
	"context"
	"errors"
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

// newDetectorWithDecoders builds a Detector whose availableDecoders() returns
// exactly the named decoders without shelling out to `ffmpeg -decoders`. It seeds
// decodersAvail and consumes decodersOnce so the cached path is taken. This lets
// the tests exercise the REAL softwareDecoderFor candidate-selection logic (the
// site of Fix B) rather than stubbing softwareDecoderFn, which would bypass it.
func newDetectorWithDecoders(decoders ...string) *Detector {
	d := &Detector{BinPath: "ffmpeg"}
	avail := make(map[string]bool, len(decoders))
	for _, dec := range decoders {
		avail[dec] = true
	}
	d.decodersAvail = avail
	d.decodersOnce.Do(func() {}) // mark cached: availableDecoders() returns the seeded set
	return d
}

// TestSoftwareDecoderFor_NativeAV1NotTrusted is the unit of Fix B: the native
// ffmpeg "av1" decoder must NOT count as a trusted verify-oracle, because it
// cannot decode the encoder's 10-bit p010 output. A host with only the native
// decoder must report "no oracle" — so the verdict becomes unverifiable, not a
// false withheld. libdav1d/libaom remain valid oracles; the complete hevc/h264
// native decoders stay trusted (the fix is surgical to av1).
//
// The existing detector_decode_verify_test.go stubs softwareDecoderFn and so
// cannot catch a regression that re-adds "av1" to the candidate list; this test
// drives the real list and would fail on exactly that regression.
func TestSoftwareDecoderFor_NativeAV1NotTrusted(t *testing.T) {
	cases := []struct {
		name     string
		decoders []string
		codec    string
		wantDec  string
		wantOK   bool
	}{
		{"av1 native-only -> no trusted oracle", []string{"av1"}, "av1", "", false},
		{"av1 none present -> no oracle", []string{}, "av1", "", false},
		{"av1 prefers libdav1d", []string{"libdav1d", "av1"}, "av1", "libdav1d", true},
		{"av1 falls back to libaom", []string{"libaom-av1", "av1"}, "av1", "libaom-av1", true},
		{"av1 libdav1d preferred over libaom", []string{"libaom-av1", "libdav1d"}, "av1", "libdav1d", true},
		{"hevc native stays trusted", []string{"hevc"}, "hevc", "hevc", true},
		{"h264 native stays trusted", []string{"h264"}, "h264", "h264", true},
		// Codec aliases must normalize before the candidate lookup. The native-only
		// alias row alone is degenerate (buggy and correct both yield ("",false)); this
		// row — alias WITH a trusted decoder present — pins normalizeRequestedCodec, so
		// dropping it (candidates[codec] instead of candidates[normalize(codec)]) fails here.
		{"av1 alias av1_vaapi resolves trusted decoder", []string{"libdav1d"}, "av1_vaapi", "libdav1d", true},
		{"av1 alias av1_vaapi native-only -> no oracle", []string{"av1"}, "av1_vaapi", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDetectorWithDecoders(tc.decoders...)
			got, ok := d.softwareDecoderFor(tc.codec)
			if got != tc.wantDec || ok != tc.wantOK {
				t.Fatalf("softwareDecoderFor(%q) with decoders %v = (%q,%v), want (%q,%v)",
					tc.codec, tc.decoders, got, ok, tc.wantDec, tc.wantOK)
			}
		})
	}
}

// TestDecodeVerifyEncode_NativeAV1IsUnverifiableNotWithheld is the both-directions
// correctness proof at the verdict layer, driving the REAL softwareDecoderFor (no
// softwareDecoderFn stub). The two negative outcomes must stay distinct — the
// whole reason the three-state verdict exists:
//
//   - only the native av1 decoder available  -> UNVERIFIABLE ("couldn't check"),
//     never withheld. This is the exact false-withheld the deploy to LXC 110
//     surfaced (container ffmpeg had no libdav1d).
//   - a TRUSTED decoder present but decode fails -> WITHHELD ("encoder produced
//     bad output"). A genuinely broken bitstream.
//   - a trusted decoder + good stats -> VERIFIED.
//
// A regression that re-trusted native av1 would let the first case reach decodeStats
// (statsUsed=true) and yield verified/withheld instead of unverifiable — in production
// the native decoder fails so it is withheld; here the stub returns good stats so it
// surfaces as verified. Either way it diverges from unverifiable and fails this test.
func TestDecodeVerifyEncode_NativeAV1IsUnverifiableNotWithheld(t *testing.T) {
	const frames = 30
	cases := []struct {
		name          string
		decoders      []string
		statsFrames   int
		statsErr      error
		wantVerdict   hardware.EncoderVerdict
		wantStatsUsed bool // a trusted decoder exists -> decodeStats is reached
	}{
		{"native-only av1 -> unverifiable (NOT withheld)", []string{"av1"}, frames, nil, hardware.VerdictUnverifiable, false},
		{"no decoder at all -> unverifiable", []string{}, frames, nil, hardware.VerdictUnverifiable, false},
		{"trusted libdav1d, decode fails -> withheld", []string{"libdav1d"}, 0, errors.New("dav1d: corrupt frame"), hardware.VerdictWithheld, true},
		{"trusted libdav1d, good output -> verified", []string{"libdav1d"}, frames, nil, hardware.VerdictVerified, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDetectorWithDecoders(tc.decoders...)
			statsUsed := false
			// decodeStatsFn is wired for every case so the unverifiable rows PROVE it
			// is never reached: with no trusted decoder the verdict must be decided
			// before any decode is attempted (that IS "couldn't verify"). A regression
			// that re-trusted native av1 would set statsUsed=true and reach
			// verified/withheld instead of unverifiable — caught by both assertions.
			d.decodeStatsFn = func(context.Context, string, string) (int, float64, float64, error) {
				statsUsed = true
				return tc.statsFrames, 120.0, 64.0, tc.statsErr
			}
			got, reason := d.decodeVerifyEncode(context.Background(), "x.av1", "av1", frames)
			if got != tc.wantVerdict {
				t.Fatalf("verdict = %q (reason: %s), want %q", got, reason, tc.wantVerdict)
			}
			if statsUsed != tc.wantStatsUsed {
				t.Fatalf("decodeStats reached = %v, want %v — native-only/no-decoder must short-circuit to unverifiable before any decode attempt",
					statsUsed, tc.wantStatsUsed)
			}
		})
	}
}
