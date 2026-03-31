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
		"-fflags":            "+genpts+discardcorrupt+igndts",
		"-avoid_negative_ts": "make_zero",
		"-c:v":               "copy",
		"-c:a":               "aac",
		"-b:a":               "192k",
		"-ac":                "2",
		"-ar":                "48000",
		"-af":                stableTranscodeAudioFilter,
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

func TestMapProfileToArgs_UsesPrimaryOptionalAVMaps(t *testing.T) {
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

	foundVideoMap := false
	foundAudioMap := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-map" && args[i+1] == "0:v:0?" {
			foundVideoMap = true
		}
		if args[i] == "-map" && args[i+1] == "0:a:0?" {
			foundAudioMap = true
		}
	}

	if !foundVideoMap || !foundAudioMap {
		t.Fatalf("expected primary optional stream maps in args, got %v", args)
	}

	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-map" && (args[i+1] == "0:v" || args[i+1] == "0:a") {
			t.Fatalf("did not expect broad stream maps in args, got %v", args)
		}
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
		"-fflags":            "+genpts+discardcorrupt+igndts",
		"-avoid_negative_ts": "make_zero",
		"-c:v":               "copy",
		"-c:a":               "aac",
		"-af":                stableTranscodeAudioFilter,
		"-b:a":               "256k",
		"-ac":                "2",
		"-ar":                "48000",
		"-hls_time":          "2",
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
		"-fflags":            "+genpts+discardcorrupt+igndts",
		"-avoid_negative_ts": "make_zero",
		"-c:v":               "libx264",
		"-preset":            "fast",
		"-crf":               "23",
		"-c:a":               "copy",
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

func TestMapProfileToArgs_TargetProfileUsesExplicitVideoLadderValues(t *testing.T) {
	spec := vod.Spec{
		Input:      "file:///tmp/input.ts",
		WorkDir:    "/tmp/work",
		OutputTemp: "index.live.m3u8",
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: playbackprofile.PackagingTS,
			Video: playbackprofile.VideoTarget{
				Mode:   playbackprofile.MediaModeTranscode,
				Codec:  "h264",
				CRF:    20,
				Preset: "slow",
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
		"-fflags":            "+genpts+discardcorrupt+igndts",
		"-avoid_negative_ts": "make_zero",
		"-c:v":               "libx264",
		"-preset":            "slow",
		"-crf":               "20",
		"-c:a":               "copy",
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

func TestMapProfileToArgs_TargetProfilePackagingFMP4DefaultsSegmentType(t *testing.T) {
	spec := vod.Spec{
		Input:      "file:///tmp/input.ts",
		WorkDir:    "/tmp/work",
		OutputTemp: "index.live.m3u8",
		TargetProfile: &playbackprofile.TargetPlaybackProfile{
			Container: "mp4",
			Packaging: playbackprofile.PackagingFMP4,
			Video: playbackprofile.VideoTarget{
				Mode: playbackprofile.MediaModeCopy,
			},
			Audio: playbackprofile.AudioTarget{
				Mode:        playbackprofile.MediaModeTranscode,
				Codec:       "aac",
				Channels:    2,
				BitrateKbps: 256,
				SampleRate:  48000,
			},
			HLS: playbackprofile.HLSTarget{
				Enabled:        true,
				SegmentSeconds: 4,
			},
		},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-fflags":                 "+genpts+discardcorrupt+igndts",
		"-avoid_negative_ts":      "make_zero",
		"-af":                     stableTranscodeAudioFilter,
		"-hls_time":               "4",
		"-hls_segment_type":       "fmp4",
		"-hls_fmp4_init_filename": "init.mp4",
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

	foundSegmentPattern := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-hls_segment_filename" && args[i+1] == filepath.Join(spec.WorkDir, "seg_%05d.m4s") {
			foundSegmentPattern = true
			break
		}
	}
	if !foundSegmentPattern {
		t.Fatalf("expected fmp4 segment filename pattern in args, got %v", args)
	}
}

func TestMapProfileToArgs_InputStabilityFlagsPrecedeInput(t *testing.T) {
	spec := vod.Spec{
		Input:      "http://receiver.invalid/recording.ts",
		WorkDir:    "/tmp/work",
		OutputTemp: "index.live.m3u8",
		Profile:    vod.ProfileDefault,
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	fflagsIdx := -1
	avoidIdx := -1
	inputIdx := -1
	for i, arg := range args {
		switch arg {
		case "-fflags":
			fflagsIdx = i
		case "-avoid_negative_ts":
			avoidIdx = i
		case "-i":
			inputIdx = i
		}
	}

	if fflagsIdx == -1 || avoidIdx == -1 || inputIdx == -1 {
		t.Fatalf("expected input stability flags and input marker in args, got %v", args)
	}
	if !(fflagsIdx < inputIdx && avoidIdx < inputIdx) {
		t.Fatalf("expected input stability flags before -i, got %v", args)
	}
}
