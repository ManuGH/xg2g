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
