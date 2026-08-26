package audiotopology

import (
	"fmt"
	"strings"
)

// DetectConflicts inspects multi-source observations for a track and records contradictions.
func DetectConflicts(
	pmt *PMTTrackObservation,
	probe *ProbeTrackObservation,
	es *ESTrackObservation,
	e2 *Enigma2TrackObservation,
) []Conflict {
	var conflicts []Conflict

	visualImpaired := (pmt != nil && (pmt.VisualImpaired || pmt.AudioType == 0x03)) ||
		(probe != nil && (probe.DispositionVisualImpaired || probe.DispositionDescriptions))

	if e2 != nil {
		e2DescLower := strings.ToLower(strings.TrimSpace(e2.Description))

		// 1. Accessibility contradiction (PMT/Probe visual_impaired vs E2 "ohne Audiodeskription")
		if visualImpaired && strings.Contains(e2DescLower, "ohne audiodeskription") {
			sourceA := EvidencePMT
			if pmt == nil && probe != nil {
				sourceA = EvidenceProbe
			}
			conflicts = append(conflicts, Conflict{
				Field:      "accessibility.audioDescription",
				SourceA:    sourceA,
				ValueA:     "visual_impaired=true",
				SourceB:    EvidenceEnigma2,
				ValueB:     e2.Description,
				Resolution: "retained_pmt_descriptor_flag_marked_conflict",
			})
		}

		// 2. Language contradiction between physical stream and Enigma2 label
		var streamLang string
		if pmt != nil && pmt.Language != "" {
			streamLang = pmt.Language
		} else if probe != nil && probe.Language != "" {
			streamLang = probe.Language
		}

		if streamLang != "" {
			streamNorm := NormalizeLanguage(streamLang).ISO639_2
			if strings.Contains(e2DescLower, "französisch") && streamNorm != "fra" && !streamNormIsGeneric(streamNorm) {
				conflicts = append(conflicts, Conflict{
					Field:      "language",
					SourceA:    EvidencePMT,
					ValueA:     streamLang,
					SourceB:    EvidenceEnigma2,
					ValueB:     e2.Description,
					Resolution: "preferred_enigma2_semantic_label",
				})
			}
		}
	}

	// 3. Channel count divergence, recorded between every pair of sources that
	//    each named a count. A source that named none cannot contradict anything -
	//    a multichannel declaration leaves the count at 0, so a descriptor saying
	//    "more than two" beside an observed 6 is agreement, not conflict.
	if pmt != nil && probe != nil {
		conflicts = appendChannelConflict(conflicts,
			EvidencePMT, pmt.Channels,
			EvidenceProbe, probe.Channels,
			"preferred_active_probe_measurement")
	}
	if es != nil {
		if pmt != nil {
			conflicts = appendChannelConflict(conflicts,
				EvidencePMT, pmt.Channels,
				EvidenceObserved, es.Channels,
				"preferred_continuous_stream_observation")
		}
		if probe != nil {
			// The two read the same syntax, so a disagreement here is not a
			// measurement error - it usually means the programme changed after the
			// probe ran, which is the case the observation exists to follow.
			conflicts = appendChannelConflict(conflicts,
				EvidenceProbe, probe.Channels,
				EvidenceObserved, es.Channels,
				"preferred_continuous_stream_observation")
		}
	}

	if pmt != nil && probe != nil {
		// 4. Codec divergence
		pmtCodec := NormalizeCodec(pmt.Codec)
		probeCodec := NormalizeCodec(probe.Codec)
		if pmtCodec != CodecUnknown && probeCodec != CodecUnknown && pmtCodec != probeCodec {
			conflicts = append(conflicts, Conflict{
				Field:      "codec",
				SourceA:    EvidencePMT,
				ValueA:     string(pmtCodec),
				SourceB:    EvidenceProbe,
				ValueB:     string(probeCodec),
				Resolution: "preferred_active_probe_measurement",
			})
		}
	}

	return conflicts
}

// appendChannelConflict records a contradiction only where both sources named a
// count. Everything else is silence, and silence contradicts nothing.
func appendChannelConflict(
	conflicts []Conflict,
	sourceA EvidenceSource, valueA int,
	sourceB EvidenceSource, valueB int,
	resolution string,
) []Conflict {
	if valueA <= 0 || valueB <= 0 || valueA == valueB {
		return conflicts
	}
	return append(conflicts, Conflict{
		Field:      "channels",
		SourceA:    sourceA,
		ValueA:     fmt.Sprintf("%d", valueA),
		SourceB:    sourceB,
		ValueB:     fmt.Sprintf("%d", valueB),
		Resolution: resolution,
	})
}

func streamNormIsGeneric(code string) bool {
	return code == "und" || code == "mis" || code == "mul" || code == ""
}
