package ffmpeg

import (
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
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
		"-fflags":            "+genpts+discardcorrupt",
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
		Intent: &ports.BuildIntent{Target: ports.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: ports.PackagingTS,
			Video: ports.VideoTarget{
				Mode:  ports.MediaModeCopy,
				Codec: "h264",
			},
			Audio: ports.AudioTarget{
				Mode:        ports.MediaModeTranscode,
				Codec:       "aac",
				Channels:    2,
				BitrateKbps: 256,
				SampleRate:  48000,
			},
			HLS: ports.HLSTarget{
				Enabled:          true,
				SegmentContainer: "mpegts",
				SegmentSeconds:   2,
			},
			HWAccel: ports.HWAccelNone,
		}},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-fflags":            "+genpts+discardcorrupt",
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
		Intent: &ports.BuildIntent{Target: ports.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: ports.PackagingTS,
			Video: ports.VideoTarget{
				Mode:  ports.MediaModeTranscode,
				Codec: "h264",
			},
			Audio: ports.AudioTarget{
				Mode:  ports.MediaModeCopy,
				Codec: "aac",
			},
			HLS: ports.HLSTarget{
				Enabled:          true,
				SegmentContainer: "mpegts",
			},
		}},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-fflags":            "+genpts+discardcorrupt",
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
		Intent: &ports.BuildIntent{Target: ports.TargetPlaybackProfile{
			Container: "mpegts",
			Packaging: ports.PackagingTS,
			Video: ports.VideoTarget{
				Mode:   ports.MediaModeTranscode,
				Codec:  "h264",
				CRF:    20,
				Preset: "slow",
			},
			Audio: ports.AudioTarget{
				Mode:  ports.MediaModeCopy,
				Codec: "aac",
			},
			HLS: ports.HLSTarget{
				Enabled:          true,
				SegmentContainer: "mpegts",
			},
		}},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-fflags":            "+genpts+discardcorrupt",
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
		Intent: &ports.BuildIntent{Target: ports.TargetPlaybackProfile{
			Container: "mp4",
			Packaging: ports.PackagingFMP4,
			Video: ports.VideoTarget{
				Mode: ports.MediaModeCopy,
			},
			Audio: ports.AudioTarget{
				Mode:        ports.MediaModeTranscode,
				Codec:       "aac",
				Channels:    2,
				BitrateKbps: 256,
				SampleRate:  48000,
			},
			HLS: ports.HLSTarget{
				Enabled:        true,
				SegmentSeconds: 4,
			},
		}},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	wantPairs := map[string]string{
		"-fflags":                 "+genpts+discardcorrupt",
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

func TestMapProfileToArgs_TargetProfileSupportsAV1VAAPI(t *testing.T) {
	spec := vod.Spec{
		Input:      "/media/nfs-recordings/test.ts",
		WorkDir:    "/tmp/work",
		OutputTemp: "index.live.m3u8",
		Intent: &ports.BuildIntent{Target: ports.TargetPlaybackProfile{
			Container: "mp4",
			Packaging: ports.PackagingFMP4,
			Video: ports.VideoTarget{
				Mode:  ports.MediaModeTranscode,
				Codec: "av1",
			},
			Audio: ports.AudioTarget{
				Mode:        ports.MediaModeTranscode,
				Codec:       "aac",
				Channels:    2,
				BitrateKbps: 320,
				SampleRate:  48000,
			},
			HLS: ports.HLSTarget{
				Enabled:          true,
				SegmentContainer: "fmp4",
				SegmentSeconds:   6,
			},
			HWAccel: ports.HWAccelVAAPI,
		}},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	joined := ""
	for _, a := range args {
		joined += " " + a
	}

	if !containsSubstring(args, "-init_hw_device") || !containsSubstring(args, "av1_vaapi") || !containsSubstring(args, "format=p010le,hwupload,scale_vaapi=format=p010:out_color_matrix=bt709:out_color_primaries=bt709:out_color_transfer=bt709") {
		t.Fatalf("expected VAAPI hardware args and av1_vaapi encoder in args, got %s", joined)
	}
}

func TestMapProfileToArgs_WithStartOffset(t *testing.T) {
	spec := vod.Spec{
		Input:         "/tmp/input.ts",
		WorkDir:       "/tmp/work",
		OutputTemp:    "index.live.m3u8",
		StartOffsetMs: 1800500, // 1800.5 seconds
		Intent: &ports.BuildIntent{
			StartOffsetMs: 1800500,
			Target: ports.TargetPlaybackProfile{
				Video: ports.VideoTarget{
					Mode:  ports.MediaModeTranscode,
					Codec: "av1",
				},
				Audio: ports.AudioTarget{
					Mode:  ports.MediaModeTranscode,
					Codec: "aac",
				},
				HLS: ports.HLSTarget{
					Enabled:          true,
					SegmentContainer: "fmp4",
				},
			},
		},
	}

	args, err := mapProfileToArgs(spec)
	if err != nil {
		t.Fatalf("mapProfileToArgs returned error: %v", err)
	}

	ssIndex := -1
	iIndex := -1
	for idx, a := range args {
		if a == "-ss" {
			ssIndex = idx
		}
		if a == "-i" {
			iIndex = idx
		}
	}

	if ssIndex == -1 || iIndex == -1 || ssIndex >= iIndex {
		t.Fatalf("expected -ss before -i in args, got ssIndex=%d, iIndex=%d, args=%v", ssIndex, iIndex, args)
	}
	if args[ssIndex+1] != "1800.5" {
		t.Fatalf("expected -ss 1800.5, got %s", args[ssIndex+1])
	}
}

func containsSubstring(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
