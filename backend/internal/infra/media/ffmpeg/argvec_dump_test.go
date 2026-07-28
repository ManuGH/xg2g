package ffmpeg

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// TestDumpLiveArgVector writes the argument vector of a live multi-audio session to
// the path in ARGVEC_DUMP, one argument per line, for scripts/smoke-live-audio.sh
// to replay. Without that variable it still asserts the audio contract, so it earns
// its place in the normal test run.
//
// The variable deliberately carries no product env prefix: it is a test harness knob,
// while cmd/configgen inventories every file that mentions a product config key as a
// config surface. A prefixed name would both pollute that inventory and read like an
// operator setting that cannot be found in the schema.
//
// The point of dumping instead of hand-writing a vector: the smoke test must exercise
// what the daemon really builds. On 2026-07-26 the audio bitrate was wrong in
// production while every hand-written expectation was satisfied, and ffmpeg's refusal
// of an invalid -var_stream_map (exit 234) is something no Go test can observe.
func TestDumpLiveArgVector(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	// Mirrors what the relay reports for a German HD channel: AC-3 5.1 primary plus
	// an AC-3 stereo secondary track.
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		return []liveAudioStream{
			{Index: 3, CodecType: "audio", CodecName: "ac3", Channels: 6, ChannelLayout: "5.1(side)", Tags: map[string]string{"language": "deu"}},
			{Index: 4, CodecType: "audio", CodecName: "ac3", Channels: 2, ChannelLayout: "stereo", Tags: map[string]string{"language": "deu"}},
		}, nil
	}

	spec := ports.StreamSpec{
		SessionID: "argvec-dump",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "av1_hw",
			Container:      "fmp4",
			VideoCodec:     "av1",
			TranscodeVideo: true,
			Deinterlace:    true,
			// The value the planner hands the pipeline for live browser playback.
			AudioBitrateK: playbackprofile.LiveTranscodeAudioBitrateKbps,
		},
		Source: ports.StreamSource{
			ID:   "http://10.10.55.64:17999/1:0:19:83:6:85:C00000:0:0:0",
			Type: ports.SourceURL,
		},
	}

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	bitrate, ok := valueAfter(args, "-b:a")
	require.True(t, ok, "vector must carry an audio bitrate")
	require.Equal(t, "320k", bitrate)
	codec, ok := valueAfter(args, "-c:a")
	require.True(t, ok)
	require.Equal(t, "aac", codec)
	varStreamMap, ok := valueAfter(args, "-var_stream_map")
	require.True(t, ok, "multi-audio session must advertise renditions")
	require.Equal(t, 1, strings.Count(varStreamMap, "a:0,"),
		"an output audio stream may belong to exactly one variant; ffmpeg rejects the header otherwise")

	dumpPath := strings.TrimSpace(os.Getenv("ARGVEC_DUMP"))
	if dumpPath == "" {
		t.Log("ARGVEC_DUMP unset; skipping vector dump")
		return
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(dumpPath), 0o750))
	// #nosec G306 -- a CI scratch file holding ffmpeg flags, no secrets involved.
	require.NoError(t, os.WriteFile(dumpPath, []byte(strings.Join(args, "\n")+"\n"), 0o644))
	t.Logf("wrote %d args to %s", len(args), dumpPath)
}
