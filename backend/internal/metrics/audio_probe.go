// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Startup audio probe outcomes, recorded once per live plan that reaches the
// probe.
//
// This is a different measurement dimension from the topology evidence next to
// it. AudioTopologyEvidenceTotal describes how two present sources of evidence
// relate to each other; this one describes whether one of them was there at all.
// Without it every evidence series would be conditioned on a successful probe,
// and the case that argues hardest for reducing the probe - it delivers nothing
// while the tables declare the tracks - would be absent from the denominator
// rather than counted.
const (
	// AudioProbeAvailable means the probe returned usable audio streams and the
	// topology classification ran on them.
	AudioProbeAvailable = "available"
	// AudioProbeFailed means the probe itself did not complete.
	AudioProbeFailed = "failed"
	// AudioProbeEmpty means the probe completed and reported no audio stream.
	// Kept apart from failed on purpose: a broken ffprobe run and a service the
	// probe saw no audio in lead to different conclusions, and folding them
	// together now would mean rebuilding this telemetry to tell them apart later.
	AudioProbeEmpty = "empty"
)

// AudioProbeTotal counts live audio plans by whether the startup probe supplied
// anything to classify.
//
// Deliberately not labelled by codec. On a failed probe there is no probe codec
// to name, and putting the PMT's codec there instead would make the label mean
// one thing in some series and another thing in the rest - which is the mixing
// of dimensions this metric exists to avoid.
//
// A path that never runs the probe is not counted here. Native Android TV skips
// it by design, so counting it would report an absence that was never asked for.
var AudioProbeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "xg2g_ingest_audio_probe_total",
	Help: "Live audio plans by whether the startup probe supplied streams to classify",
}, []string{"result"})

// IncAudioProbe records one live audio plan's probe outcome.
func IncAudioProbe(result string) {
	AudioProbeTotal.WithLabelValues(result).Inc()
}
