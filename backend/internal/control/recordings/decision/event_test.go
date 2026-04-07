package decision

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/stretchr/testify/require"
)

func TestBuildEvent_BasisHashIgnoresHostFingerprint(t *testing.T) {
	input := DecisionInput{
		Source: Source{
			Container:  "ts",
			VideoCodec: "h264",
			AudioCodec: "aac",
			Width:      1920,
			Height:     1080,
			FPS:        50,
		},
		Capabilities: Capabilities{
			Version:     3,
			Containers:  []string{"hls"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac"},
			SupportsHLS: true,
			DeviceType:  "web",
		},
		Policy: Policy{
			AllowTranscode: true,
			Host:           HostPolicy{PressureBand: playbackprofile.HostPressureNormal},
		},
		RequestedIntent: playbackprofile.IntentQuality,
		APIVersion:      "v3.1",
		RequestID:       "req-1",
	}
	decision := &Decision{
		Mode: ModeTranscode,
		Selected: SelectedFormats{
			Container:  "fmp4",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
		Reasons: []ReasonCode{ReasonVideoCodecNotSupported},
		Trace: Trace{
			RequestedIntent:  "quality",
			ResolvedIntent:   "quality",
			HostPressureBand: string(playbackprofile.HostPressureNormal),
		},
	}

	eventA, err := BuildEvent(EventMetadata{
		ServiceRef:       "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind:      "live",
		Origin:           OriginRuntime,
		ClientFamily:     "safari_native",
		ClientCapsSource: "runtime",
		DeviceType:       "web",
		HostFingerprint:  "df1:host-a",
		DecidedAt:        time.Unix(1_700_000_000, 0).UTC(),
	}, input, decision)
	require.NoError(t, err)

	eventB, err := BuildEvent(EventMetadata{
		ServiceRef:       "1:0:1:2B66:3F3:1:C00000:0:0:0:",
		SubjectKind:      "live",
		Origin:           OriginRuntime,
		ClientFamily:     "safari_native",
		ClientCapsSource: "runtime",
		DeviceType:       "web",
		HostFingerprint:  "df1:host-b",
		DecidedAt:        time.Unix(1_700_000_000, 0).UTC(),
	}, input, decision)
	require.NoError(t, err)

	require.Equal(t, eventA.BasisHash, eventB.BasisHash)
	require.NotEqual(t, eventA.HostFingerprint, eventB.HostFingerprint)
}
