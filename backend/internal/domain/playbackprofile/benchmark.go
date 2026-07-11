package playbackprofile

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
)

const (
	BenchmarkProfileAudioAACStereo   = ports.BenchmarkProfileAudioAACStereo
	BenchmarkProfileVideoH2641080P   = ports.BenchmarkProfileVideoH2641080P
	BenchmarkProfileVideoH2641080I   = ports.BenchmarkProfileVideoH2641080I
	BenchmarkProfileVideoH2641080I50 = ports.BenchmarkProfileVideoH2641080I50
	BenchmarkProfileVideoH2642160P   = ports.BenchmarkProfileVideoH2642160P
	BenchmarkProfileVideoH2642160P50 = ports.BenchmarkProfileVideoH2642160P50
	BenchmarkProfileVideoAV11080I50  = ports.BenchmarkProfileVideoAV11080I50
)

// BenchmarkClassForCodec returns the most specific measured benchmark class for a codec.
// If no codec-specific benchmark exists, it falls back to the aggregate benchmark class.
func BenchmarkClassForCodec(snapshot ports.HostBenchmarkSnapshot, codec string) string {
	return ports.BenchmarkClassForCodec(snapshot, codec)
}

// BenchmarkClassForProfile returns the most specific benchmark class for a transcode profile.
// If no profile-specific benchmark exists, it falls back to the aggregate benchmark class.
func BenchmarkClassForProfile(snapshot ports.HostBenchmarkSnapshot, profileID string) string {
	return ports.BenchmarkClassForProfile(snapshot, profileID)
}
