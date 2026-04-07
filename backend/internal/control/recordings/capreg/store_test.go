package capreg

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHostSnapshotDecisionFingerprint_UsesStableHostClass(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			Hostname:     "host-a",
			OSName:       "Linux",
			OSVersion:    "6.9 ",
			Architecture: "amd64",
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "hevc", Verified: true, AutoEligible: false, ProbeElapsedMS: 51},
			{Codec: "h264", Verified: false, AutoEligible: true, ProbeElapsedMS: 12},
		},
		UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}

	changedRuntime := base
	changedRuntime.Identity.Hostname = "host-b"
	changedRuntime.UpdatedAt = time.Unix(1_800_000_000, 0).UTC()
	changedRuntime.EncoderCapabilities = []EncoderCapability{
		{Codec: "h264", Verified: true, AutoEligible: false, ProbeElapsedMS: 999},
		{Codec: "hevc", Verified: false, AutoEligible: true, ProbeElapsedMS: 1},
	}

	baseFingerprint := base.DecisionFingerprint()
	require.NotEmpty(t, baseFingerprint)
	require.Contains(t, baseFingerprint, "df1:")
	require.Equal(t, baseFingerprint, changedRuntime.DecisionFingerprint())
}

func TestHostSnapshotDecisionFingerprint_ChangesWhenHostClassChanges(t *testing.T) {
	base := HostSnapshot{
		Identity: HostIdentity{
			OSName:       "linux",
			OSVersion:    "6.9",
			Architecture: "amd64",
		},
		EncoderCapabilities: []EncoderCapability{
			{Codec: "hevc"},
			{Codec: "h264"},
		},
	}

	withAV1 := base
	withAV1.EncoderCapabilities = append(withAV1.EncoderCapabilities, EncoderCapability{Codec: "av1"})

	require.NotEqual(t, base.DecisionFingerprint(), withAV1.DecisionFingerprint())
}
