// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Audio topology evidence outcomes. One series per planned audio track, recorded
// where the PMT declaration and the ffprobe observation sit next to each other.
//
// The question these answer is whether the declarations that shared ingest reads
// out of the PMT are enough on their own, or whether the startup probe is still
// carrying the load. DVB descriptors may omit the channel information, and when
// they carry it they name a class rather than a count, so the answer cannot be
// reasoned out - only counted, on real services.
const (
	// AudioEvidenceDeclaredExact means the PMT named a channel count.
	AudioEvidenceDeclaredExact = "declared_exact"
	// AudioEvidenceDeclaredMulti means the PMT declared more than two channels
	// without naming how many. Deliberately its own outcome: it is neither a
	// number nor silence, and folding it into either would answer the question
	// this metric exists to ask.
	AudioEvidenceDeclaredMulti = "declared_multi"
	// AudioEvidenceDeclaredNone means the PMT carried no usable channel
	// information for this track.
	AudioEvidenceDeclaredNone = "declared_none"
	// AudioEvidenceProbeOnly means only ffprobe saw this track.
	AudioEvidenceProbeOnly = "probe_only"
	// AudioEvidencePMTOnly means only the PMT declared this track. It is an
	// observation, not a fault: a short startup snapshot can simply not have
	// reached a track the tables already name.
	AudioEvidencePMTOnly = "pmt_only"
	// AudioEvidenceConflict means both sources named a channel count and the two
	// disagreed.
	AudioEvidenceConflict = "conflict"
)

// AudioTopologyEvidenceTotal counts what each planned audio track's channel
// information came from.
//
// Labelled by codec and outcome only. A PID, a language or a service name would
// each multiply the series by the size of the channel list for no gain: the
// decision this feeds - can the probe be reduced - is per codec, because whether
// a descriptor carries channels is a property of the codec's descriptor, not of
// the individual service.
var AudioTopologyEvidenceTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "xg2g_ingest_audio_topology_evidence_total",
	Help: "Planned audio tracks by codec and where their channel information came from",
}, []string{"codec", "result"})

// IncAudioTopologyEvidence records one planned audio track's evidence outcome.
func IncAudioTopologyEvidence(codec, result string) {
	if codec == "" {
		codec = "unknown"
	}
	AudioTopologyEvidenceTotal.WithLabelValues(codec, result).Inc()
}
