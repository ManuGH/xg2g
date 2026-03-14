package playbackprofile

import "testing"

func TestHashTarget_SemanticNormalization(t *testing.T) {
	a := TargetPlaybackProfile{
		Container: " MPEGTS ",
		Packaging: Packaging("TS"),
		Video: VideoTarget{
			Mode:        MediaMode("TRANSCODE"),
			Codec:       "H264",
			BitrateKbps: 4000,
			Width:       1920,
			Height:      1080,
			FPS:         25,
		},
		Audio: AudioTarget{
			Mode:        MediaMode("COPY"),
			Codec:       "AAC",
			Channels:    2,
			BitrateKbps: 256,
			SampleRate:  48000,
		},
		HLS: HLSTarget{
			Enabled:          true,
			SegmentContainer: "MPEGTS",
			SegmentSeconds:   2,
		},
	}

	b := TargetPlaybackProfile{
		Container: "mpegts",
		Packaging: PackagingTS,
		Video: VideoTarget{
			Mode:        MediaModeTranscode,
			Codec:       "h264",
			BitrateKbps: 4000,
			Width:       1920,
			Height:      1080,
			FPS:         25,
		},
		Audio: AudioTarget{
			Mode:        MediaModeCopy,
			Codec:       "aac",
			Channels:    2,
			BitrateKbps: 256,
			SampleRate:  48000,
		},
		HLS: HLSTarget{
			Enabled:          true,
			SegmentContainer: "mpegts",
			SegmentSeconds:   2,
		},
		HWAccel: HWAccelNone,
	}

	if HashTarget(a) != HashTarget(b) {
		t.Fatalf("expected semantically equal target profiles to hash identically: %q != %q", HashTarget(a), HashTarget(b))
	}
}

func TestHashTarget_DetectsMeaningfulDifferences(t *testing.T) {
	base := TargetPlaybackProfile{
		Container: "mpegts",
		Packaging: PackagingTS,
		Video: VideoTarget{
			Mode:  MediaModeCopy,
			Codec: "h264",
		},
		Audio: AudioTarget{
			Mode:        MediaModeTranscode,
			Codec:       "aac",
			Channels:    2,
			BitrateKbps: 256,
			SampleRate:  48000,
		},
		HWAccel: HWAccelNone,
	}

	changed := base
	changed.Audio.BitrateKbps = 320

	if HashTarget(base) == HashTarget(changed) {
		t.Fatal("expected bitrate change to produce a distinct target hash")
	}
}
