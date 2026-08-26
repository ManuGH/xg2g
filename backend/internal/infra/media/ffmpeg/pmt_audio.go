// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"github.com/ManuGH/xg2g/internal/domain/audiotopology"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// pmtTrackObservations turns the PMT's audio declarations into the domain's PMT
// observations.
//
// BuildTopology has taken pmtTracks since it was written and has been handed nil
// for as long: nothing could supply them, because the media path had no view of
// the transport stream's own tables. Shared ingest does, so this fills an argument
// that has always existed rather than adding a path.
//
// The merge resolves the two sources itself - a probe observation still overrides
// a PMT declaration for the same PID, and a disagreement is recorded as a conflict
// - so this deliberately makes no decision beyond translating.
func pmtTrackObservations(tracks []ports.LiveAudioTrack) []audiotopology.PMTTrackObservation {
	if len(tracks) == 0 {
		return nil
	}
	out := make([]audiotopology.PMTTrackObservation, 0, len(tracks))
	for _, t := range tracks {
		out = append(out, audiotopology.PMTTrackObservation{
			PID:        t.PID,
			StreamType: t.StreamType,
			Codec:      t.Codec,
			Language:   t.Language,
			// Channels stays 0 for a multichannel declaration on purpose. The
			// merge treats 0 as "not declared" and lets the probe answer, which is
			// correct: the descriptor said "more than two" and inventing 6 here
			// would put a number in front of the encoder that the stream never
			// gave. What the declaration does carry either way is the codec and
			// the language, and those are exact.
			Channels: t.Channels,
		})
	}
	return out
}
