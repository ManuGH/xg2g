// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"github.com/ManuGH/xg2g/internal/domain/audiotopology"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/metrics"
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

// audioEvidence is one planned track's codec and where its channel information
// came from.
type audioEvidence struct {
	codec  string
	result string
}

// classifyAudioEvidence reports, per audio track, which source supplied the
// channel information and whether the two sources agreed.
//
// It exists to answer a question that cannot be reasoned out: DVB descriptors may
// omit the channel information, and where they carry it they name a class rather
// than a count, so how far the declarations alone would carry can only be counted
// on real services. Nothing here influences the plan.
//
// The union of both sources is walked, not just the intersection, because a track
// only one of them knows about is itself one of the answers.
func classifyAudioEvidence(pmt []ports.LiveAudioTrack, probes map[uint16]liveAudioStream) []audioEvidence {
	if len(pmt) == 0 && len(probes) == 0 {
		return nil
	}

	out := make([]audioEvidence, 0, len(pmt)+len(probes))
	seen := make(map[uint16]struct{}, len(pmt))

	for _, track := range pmt {
		seen[track.PID] = struct{}{}
		probe, probed := probes[track.PID]
		if !probed {
			// Not a fault. A startup snapshot is short, and a track the tables
			// already name can simply not have arrived in it yet.
			out = append(out, audioEvidence{codec: track.Codec, result: metrics.AudioEvidencePMTOnly})
			continue
		}
		out = append(out, audioEvidence{codec: track.Codec, result: declaredResult(track, probe)})
	}

	for pid, probe := range probes {
		if _, ok := seen[pid]; ok {
			continue
		}
		out = append(out, audioEvidence{codec: probe.CodecName, result: metrics.AudioEvidenceProbeOnly})
	}
	return out
}

// declaredResult classifies a track both sources know about.
func declaredResult(track ports.LiveAudioTrack, probe liveAudioStream) string {
	switch {
	case track.Channels > 0 && probe.Channels > 0 && track.Channels != probe.Channels:
		return metrics.AudioEvidenceConflict
	case track.Channels > 0:
		return metrics.AudioEvidenceDeclaredExact
	case track.Multichannel:
		// Its own outcome on purpose. "More than two, count unstated" is neither a
		// number nor silence, and folding it into either would answer the question
		// this counter exists to ask.
		return metrics.AudioEvidenceDeclaredMulti
	default:
		return metrics.AudioEvidenceDeclaredNone
	}
}

// recordAudioEvidence publishes the classification. Measurement only.
func recordAudioEvidence(pmt []ports.LiveAudioTrack, probes map[uint16]liveAudioStream) {
	for _, ev := range classifyAudioEvidence(pmt, probes) {
		metrics.IncAudioTopologyEvidence(ev.codec, ev.result)
	}
}
