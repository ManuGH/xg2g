package ffmpeg

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAV1VAAPIGeometryPadFilter_UpscaleClause guards the AV1 geometry filter
// string: it must upscale to at least 720 lines (so Apple's M-series AV1 HW
// decoder does not black out on SD AV1) while keeping the existing 16-line pad
// workaround for the AMD 1080->1082 decode quirk.
func TestAV1VAAPIGeometryPadFilter_UpscaleClause(t *testing.T) {
	f := av1VAAPIGeometryPadFilter()

	// Upscale to >= 720, square pixels via display-aspect-derived width, only
	// for sub-720 sources (max(720,ih) is a no-op for HD).
	assert.Contains(t, f, "scale=", "must scale the source")
	assert.Contains(t, f, `max(720\,ih)`, "must target at least 720 lines (escaped comma keeps it inside the expression)")
	assert.Contains(t, f, "dar", "width must be derived from the display aspect ratio for square pixels")

	// Existing 16-line pad workaround must be preserved.
	assert.Contains(t, f, "pad=iw:ceil(ih/16)*16:0:(oh-ih)/2:black", "must keep the 16-line height pad")
	assert.Contains(t, f, "setsar=sar=sar*ceil(h/16)*16/h", "must keep SAR compensation for the pad")
}

// TestVaapiEncodeOnlyFilter_GeometryUpscaleIsAV1Only ensures the geometry
// upscale is applied for AV1 only and never for H.264/HEVC, which decode SD
// fine and must not be needlessly upscaled.
func TestVaapiEncodeOnlyFilter_GeometryUpscaleIsAV1Only(t *testing.T) {
	a := &LocalAdapter{}
	spec := ports.StreamSpec{
		Mode:    ports.ModeLive,
		Format:  ports.FormatHLS,
		Profile: model.ProfileSpec{Name: "test", Deinterlace: false},
	}

	av1 := a.vaapiEncodeOnlyFilter(spec, "av1")
	assert.Contains(t, av1, av1VAAPIGeometryPadFilter(), "av1 must get the geometry upscale/pad")
	assert.Contains(t, av1, "format=p010le,hwupload")

	for _, codec := range []string{"h264", "hevc"} {
		got := a.vaapiEncodeOnlyFilter(spec, codec)
		assert.NotContains(t, got, "max(720", "%s must not be geometry-upscaled", codec)
		assert.NotContains(t, got, "pad=iw:ceil(ih/16)*16", "%s must not get the AV1 pad", codec)
	}
}

var showinfoSizeRE = regexp.MustCompile(`s:(\d+)x(\d+)`)

// runGeometryFilter feeds a synthetic source through the real AV1 geometry
// filter and returns the resulting frame dimensions, using showinfo. It is the
// behavioural net proving the filter math (SD -> >=720, HD -> 1088) rather than
// just asserting the filter string.
func runGeometryFilter(t *testing.T, ffmpeg, source string) (w, h int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	graph := source + "," + av1VAAPIGeometryPadFilter() + ",showinfo"
	// #nosec G204 -- ffmpeg path is resolved via LookPath; graph is test-controlled.
	out, err := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "info",
		"-f", "lavfi", "-i", graph,
		"-frames:v", "1", "-f", "null", "-",
	).CombinedOutput()
	require.NoErrorf(t, err, "ffmpeg failed: %s", string(out))

	matches := showinfoSizeRE.FindAllStringSubmatch(string(out), -1)
	require.NotEmpty(t, matches, "no showinfo size in ffmpeg output: %s", string(out))
	last := matches[len(matches)-1]
	w, _ = strconv.Atoi(last[1])
	h, _ = strconv.Atoi(last[2])
	return w, h
}

// TestAV1VAAPIGeometryPadFilter_FFmpegGeometry proves the actual output
// geometry on real ffmpeg. Skipped when ffmpeg is unavailable so it never
// blocks pure unit runs/CI without ffmpeg.
func TestAV1VAAPIGeometryPadFilter_FFmpegGeometry(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not available; skipping behavioural geometry test")
	}

	cases := []struct {
		name         string
		source       string
		wantW, wantH int
	}{
		{"sd 720x576 16:9 -> 1280x720", "testsrc=size=720x576:rate=25,setsar=64/45", 1280, 720},
		{"sd 720x576 4:3 -> 960x720", "testsrc=size=720x576:rate=25,setsar=16/15", 960, 720},
		{"sd 704x576 4:3 -> 960x720", "testsrc=size=704x576:rate=25,setsar=12/11", 960, 720},
		{"hd 1920x1080 -> 1920x1088 (pad preserved)", "testsrc=size=1920x1080:rate=25,setsar=1/1", 1920, 1088},
		{"720p 1280x720 -> unchanged", "testsrc=size=1280x720:rate=25,setsar=1/1", 1280, 720},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			w, h := runGeometryFilter(t, ffmpeg, tc.source)
			assert.GreaterOrEqual(t, h, 720, "AV1 output must be at least 720 lines")
			assert.Equal(t, tc.wantW, w, "output width")
			assert.Equal(t, tc.wantH, h, "output height")
		})
	}
}
