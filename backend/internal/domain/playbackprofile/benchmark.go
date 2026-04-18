package playbackprofile

import "github.com/ManuGH/xg2g/internal/normalize"

const (
	BenchmarkProfileAudioAACStereo   = "audio_aac_stereo"
	BenchmarkProfileVideoH2641080P   = "video_h264_1080p"
	BenchmarkProfileVideoH2641080I   = "video_h264_1080i"
	BenchmarkProfileVideoH2641080I50 = "video_h264_1080i50"
	BenchmarkProfileVideoH2642160P   = "video_h264_2160p"
	BenchmarkProfileVideoH2642160P50 = "video_h264_2160p50"
)

// BenchmarkClassForCodec returns the most specific measured benchmark class for a codec.
// If no codec-specific benchmark exists, it falls back to the aggregate benchmark class.
func BenchmarkClassForCodec(snapshot HostBenchmarkSnapshot, codec string) string {
	codec = normalize.Token(codec)
	fallback := normalize.Token(snapshot.Class)
	if codec == "" {
		return fallback
	}

	for _, benchmark := range snapshot.Codecs {
		if normalize.Token(benchmark.Codec) != codec {
			continue
		}
		if class := normalize.Token(benchmark.Class); class != "" {
			return class
		}
		break
	}

	return fallback
}

// BenchmarkClassForProfile returns the most specific benchmark class for a transcode profile.
// If no profile-specific benchmark exists, it falls back to the aggregate benchmark class.
func BenchmarkClassForProfile(snapshot HostBenchmarkSnapshot, profileID string) string {
	profileID = normalize.Token(profileID)
	fallback := normalize.Token(snapshot.Class)
	if profileID == "" {
		return fallback
	}

	for _, benchmark := range snapshot.Profiles {
		if normalize.Token(benchmark.ProfileID) != profileID {
			continue
		}
		if class := normalize.Token(benchmark.Class); class != "" {
			return class
		}
		break
	}

	return fallback
}
