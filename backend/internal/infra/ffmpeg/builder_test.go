package ffmpeg

import (
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/vod"
)

func TestMapProfileToArgs_ProfileDefaultEnsuresAACAudio(t *testing.T) {
	spec := vod.Spec{
		Input:      "file:///tmp/input.ts",
		WorkDir:    "/tmp/work",
		OutputTemp: "index.live.m3u8",
		Profile:    vod.ProfileDefault,
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-c:v": "copy",
		"-c:a": "aac",
		"-b:a": "192k",
		"-ac":  "2",
		"-ar":  "48000",
	}
	for flag, want := range wantPairs {
		found := false
		for i := 0; i < len(args)-1; i++ {
			if args[i] == flag && args[i+1] == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s %s in args, got %v", flag, want, args)
		}
	}

	outputPath := filepath.Join(spec.WorkDir, spec.OutputTemp)
	if got := args[len(args)-1]; got != outputPath {
		t.Fatalf("expected output path %q, got %q", outputPath, got)
	}
}

func TestMapProfileToArgs_TargetProfileOverridesLegacyProfile(t *testing.T) {
	spec := vod.Spec{
		Input:      "file:///tmp/input.ts",
		WorkDir:    "/tmp/work",
		OutputTemp: "index.live.m3u8",
		Profile:    vod.ProfileLow,
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: playbackprofile.PackagingTS,
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: "h264",
			},
			Audio: playbackprofile.AudioTarget{
				Mode:        playbackprofile.MediaModeTranscode,
				Codec:       "aac",
				Channels:    2,
				BitrateKbps: 256,
				SampleRate:  48000,
			},
			HLS: playbackprofile.HLSTarget{
				Enabled:          true,
				SegmentContainer: "mpegts",
				SegmentSeconds:   2,
			},
			HWAccel: playbackprofile.HWAccelNone,
		},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-c:v":      "copy",
		"-c:a":      "aac",
		"-b:a":      "256k",
		"-ac":       "2",
		"-ar":       "48000",
		"-hls_time": "2",
	}
	for flag, want := range wantPairs {
		found := false
		for i := 0; i < len(args)-1; i++ {
			if args[i] == flag && args[i+1] == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s %s in args, got %v", flag, want, args)
		}
	}

	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-preset" && args[i+1] == "fast" {
			t.Fatalf("did not expect legacy low-profile preset when target profile is present: %v", args)
		}
	}
}

func TestMapProfileToArgs_TargetProfileCanTranscodeVideoAndCopyAudio(t *testing.T) {
	spec := vod.Spec{
		Input:      "file:///tmp/input.ts",
		WorkDir:    "/tmp/work",
		OutputTemp: "index.live.m3u8",
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: playbackprofile.PackagingTS,
			Video: playbackprofile.VideoTarget{
				Mode:  playbackprofile.MediaModeTranscode,
				Codec: "h264",
			},
			Audio: playbackprofile.AudioTarget{
				Mode:  playbackprofile.MediaModeCopy,
				Codec: "aac",
			},
			HLS: playbackprofile.HLSTarget{
				Enabled:          true,
				SegmentContainer: "mpegts",
			},
		},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-c:v":    "libx264",
		"-preset": "fast",
		"-crf":    "23",
		"-c:a":    "copy",
	}
	for flag, want := range wantPairs {
		found := false
		for i := 0; i < len(args)-1; i++ {
			if args[i] == flag && args[i+1] == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %s %s in args, got %v", flag, want, args)
		}
	}
}
