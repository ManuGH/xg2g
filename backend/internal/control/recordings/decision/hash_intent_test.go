package decision

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestComputeHash_RequestedIntentChangesSemanticHash(t *testing.T) {
	base := DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:     1,
			Containers:  []string{"mp4"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac"},
			SupportsHLS: true,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	compatible := base
	compatible.RequestedIntent = playbackprofile.IntentCompatible

	quality := base
	quality.RequestedIntent = playbackprofile.IntentQuality

	if compatible.ComputeHash() == quality.ComputeHash() {
		t.Fatal("expected requested intent to affect semantic hash")
	}
}

func TestComputeHash_HostPressureChangesSemanticHash(t *testing.T) {
	base := DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:     1,
			Containers:  []string{"mp4"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac"},
			SupportsHLS: true,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	normal := base
	normal.Policy.Host.PressureBand = playbackprofile.HostPressureNormal

	constrained := base
	constrained.Policy.Host.PressureBand = playbackprofile.HostPressureConstrained

	if normal.ComputeHash() == constrained.ComputeHash() {
		t.Fatal("expected host pressure to affect semantic hash")
	}
}

func TestComputeHash_HostPerformanceClassChangesSemanticHash(t *testing.T) {
	base := DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:     1,
			Containers:  []string{"mp4"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac"},
			SupportsHLS: true,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	low := base
	low.Policy.Host.PerformanceClass = "low"

	high := base
	high.Policy.Host.PerformanceClass = "high"

	if low.ComputeHash() == high.ComputeHash() {
		t.Fatal("expected host performance class to affect semantic hash")
	}
}

func TestComputeHash_HostBenchmarkClassChangesSemanticHash(t *testing.T) {
	base := DecisionInput{
		Source: Source{
			Container:  "mp4",
			VideoCodec: "h264",
			AudioCodec: "ac3",
		},
		Capabilities: Capabilities{
			Version:     1,
			Containers:  []string{"mp4"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac"},
			SupportsHLS: true,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	weak := base
	weak.Policy.Host.BenchmarkClass = "weak"

	strong := base
	strong.Policy.Host.BenchmarkClass = "strong"

	if weak.ComputeHash() == strong.ComputeHash() {
		t.Fatal("expected host benchmark class to affect semantic hash")
	}
}

func TestComputeHash_BitrateConfidenceChangesSemanticHash(t *testing.T) {
	base := DecisionInput{
		Source: Source{
			Container:         "mp4",
			VideoCodec:        "h264",
			AudioCodec:        "ac3",
			BitrateKbps:       9000,
			BitrateConfidence: "low",
		},
		Capabilities: Capabilities{
			Version:     1,
			Containers:  []string{"mp4"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac"},
			SupportsHLS: true,
		},
		Policy:     Policy{AllowTranscode: true},
		APIVersion: "v3",
	}

	high := base
	high.Source.BitrateConfidence = "high"

	if base.ComputeHash() == high.ComputeHash() {
		t.Fatal("expected bitrate confidence to affect semantic hash")
	}
}
