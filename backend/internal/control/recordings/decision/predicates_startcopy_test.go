package decision

import "testing"

func TestCanKeepVideoCopy_CompatibleProgressiveSourceReturnsTrue(t *testing.T) {
	if !CanKeepVideoCopy(
		Source{
			VideoCodec: "h264",
			Width:      1920,
			Height:     1080,
			FPS:        25,
			Interlaced: false,
		},
		Capabilities{
			VideoCodecs: []string{"h264", "hevc"},
			MaxVideo: &MaxVideoDimensions{
				Width:  3840,
				Height: 2160,
				FPS:    60,
			},
		},
	) {
		t.Fatal("CanKeepVideoCopy() = false, want true")
	}
}

func TestCanKeepVideoCopy_InterlacedOrOversizedSourceReturnsFalse(t *testing.T) {
	tests := []struct {
		name   string
		source Source
	}{
		{
			name: "interlaced",
			source: Source{
				VideoCodec: "h264",
				Width:      1920,
				Height:     1080,
				FPS:        25,
				Interlaced: true,
			},
		},
		{
			name: "resolution exceeds limit",
			source: Source{
				VideoCodec: "h264",
				Width:      3840,
				Height:     2160,
				FPS:        25,
				Interlaced: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if CanKeepVideoCopy(
				tt.source,
				Capabilities{
					VideoCodecs: []string{"h264"},
					MaxVideo: &MaxVideoDimensions{
						Width:  1920,
						Height: 1080,
						FPS:    60,
					},
				},
			) {
				t.Fatal("CanKeepVideoCopy() = true, want false")
			}
		})
	}
}

func TestCanKeepAudioCopy_DependsOnAudioCodecSupport(t *testing.T) {
	if !CanKeepAudioCopy(
		Source{AudioCodec: "ac3"},
		Capabilities{AudioCodecs: []string{"aac", "ac3"}},
	) {
		t.Fatal("CanKeepAudioCopy() = false, want true")
	}

	if CanKeepAudioCopy(
		Source{AudioCodec: "eac3"},
		Capabilities{AudioCodecs: []string{"aac", "ac3"}},
	) {
		t.Fatal("CanKeepAudioCopy() = true, want false")
	}
}
