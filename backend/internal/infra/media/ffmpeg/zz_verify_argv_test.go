package ffmpeg

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

func zzAdapter(t *testing.T, dvr time.Duration) *LocalAdapter {
	t.Helper()
	return NewLocalAdapterWithConfig(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", dvr, 0, false, 2*time.Second, 6, 0, 0, "",
		LoadAdapterConfig("", ""),
	)
}

func zzSpec(container string) ports.StreamSpec {
	return ports.StreamSpec{
		SessionID: "zz-sid",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Source: ports.StreamSource{
			ID:   "http://receiver.local:8001/1:0:19:132F:3EF:1:C00000:0:0:0:",
			Type: ports.SourceURL,
		},
		Profile: ports.ProfileSpec{
			Name:           "android_tv_native",
			TranscodeVideo: false,
			AudioBitrateK:  320,
			Container:      container,
			PolicyModeHint: ports.RuntimeModeCopy,
		},
	}
}

func zzDump(t *testing.T, label string, args []string) {
	t.Helper()
	t.Logf("### %s (n=%d)\n%s", label, len(args), strings.Join(args, " \x1f "))
}

func TestZZCaptureArgv(t *testing.T) {
	ctx := context.Background()

	a0 := zzAdapter(t, 0)
	tsArgs, err := a0.buildArgs(ctx, zzSpec("mpegts"), zzSpec("mpegts").Source.ID)
	if err != nil {
		t.Fatal(err)
	}
	zzDump(t, "TS dvr=0", tsArgs)

	fmArgs, err := a0.buildArgs(ctx, zzSpec("fmp4"), zzSpec("fmp4").Source.ID)
	if err != nil {
		t.Fatal(err)
	}
	zzDump(t, "FMP4 dvr=0", fmArgs)

	a45 := zzAdapter(t, 45*time.Minute)
	tsArgs45, err := a45.buildArgs(ctx, zzSpec("mpegts"), zzSpec("mpegts").Source.ID)
	if err != nil {
		t.Fatal(err)
	}
	zzDump(t, "TS dvr=45m (production default)", tsArgs45)

	// diff TS vs FMP4
	t.Logf("### DIFF TS->FMP4")
	i, j := 0, 0
	for i < len(tsArgs) || j < len(fmArgs) {
		switch {
		case i < len(tsArgs) && j < len(fmArgs) && tsArgs[i] == fmArgs[j]:
			i++
			j++
		case i < len(tsArgs) && (j >= len(fmArgs) || !contains(fmArgs[j:], tsArgs[i])):
			t.Logf("  TS-only  @%d: %q", i, tsArgs[i])
			i++
		default:
			if j < len(fmArgs) {
				t.Logf("  FMP4-only@%d: %q", j, fmArgs[j])
				j++
			} else {
				i++
			}
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
