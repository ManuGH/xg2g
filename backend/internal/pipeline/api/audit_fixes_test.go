package api

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// L16: LastSegmentAtUnix is stored in milliseconds (set via now.UnixMilli), so it must NOT
// be multiplied by 1000 again (which produced microseconds). LastPlaylistAtUnix is stored
// in seconds (now.Unix), so its *1000 conversion is correct and must be preserved.
func TestDeriveEvidence_SegmentTimestampUnits(t *testing.T) {
	const playlistSec = int64(1_700)
	const segmentMs = int64(1_700_000)

	trace := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 1,
			LastPlaylistAtUnix:   playlistSec,
			SegmentRequestCount:  1,
			LastSegmentAtUnix:    segmentMs,
		},
	}

	ev := deriveSessionPlaybackHealthEvidence(trace, SessionPlaybackHealthContext{})

	if ev.LastSegmentAtMs != segmentMs {
		t.Fatalf("LastSegmentAtMs: already-ms value must not be re-multiplied; got %d, want %d", ev.LastSegmentAtMs, segmentMs)
	}
	if ev.LastPlaylistAtMs != playlistSec*1000 {
		t.Fatalf("LastPlaylistAtMs: seconds must convert to ms; got %d, want %d", ev.LastPlaylistAtMs, playlistSec*1000)
	}
}
