package vod

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// L9: a legitimate large duration change (e.g. a re-cut/transcode) must be accepted. The
// old MarkProbed guard rejected any change larger than 50% of the stored value, pinning a
// stale duration.
func TestManager_MarkProbed_AcceptsLargeDurationChange(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)
	id := "recut:ref"

	// Stored truth: a 3600s recording.
	mgr.SeedMetadata(id, Metadata{State: ArtifactStateReady, Duration: 3600})

	// Re-probe finds 100s (a >50% change). Must be accepted, not rejected.
	info := &StreamInfo{Video: VideoStreamInfo{CodecName: "h264", Duration: 100}}
	mgr.MarkProbed(id, "", info, nil)

	finalMeta, ok := mgr.GetMetadata(id)
	require.True(t, ok)
	if finalMeta.Duration != 100 {
		t.Fatalf("expected duration updated to 100 for a large legitimate change, got %d", finalMeta.Duration)
	}
}
