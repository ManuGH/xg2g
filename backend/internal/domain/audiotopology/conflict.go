package audiotopology

import (
	"fmt"
	"strings"
)

// DetectConflicts inspects multi-source observations for a track and records contradictions.
func DetectConflicts(
	pmt *PMTTrackObservation,
	probe *ProbeTrackObservation,
	e2 *Enigma2TrackObservation,
) []Conflict {
	var conflicts []Conflict

	if pmt != nil && e2 != nil {
		// 1. Accessibility contradiction (e.g. arte PID 5117: PMT visual_impaired vs E2 "ohne Audiodeskription")
		e2DescLower := strings.ToLower(strings.TrimSpace(e2.Description))
		if pmt.VisualImpaired && strings.Contains(e2DescLower, "ohne audiodeskription") {
			conflicts = append(conflicts, Conflict{
				Field:      "accessibility.audioDescription",
				SourceA:    EvidencePMT,
				ValueA:     "visual_impaired=true",
				SourceB:    EvidenceEnigma2,
				ValueB:     e2.Description,
				Resolution: "retained_pmt_descriptor_flag_marked_conflict",
			})
		}

		// 2. Language contradiction
		if strings.Contains(e2DescLower, "französisch") && pmt.Language != "" && pmt.Language != "fra" && pmt.Language != "fre" && pmt.Language != "und" && pmt.Language != "mis" {
			conflicts = append(conflicts, Conflict{
				Field:      "language",
				SourceA:    EvidencePMT,
				ValueA:     pmt.Language,
				SourceB:    EvidenceEnigma2,
				ValueB:     e2.Description,
				Resolution: "preferred_enigma2_semantic_label",
			})
		}
	}

	if pmt != nil && probe != nil {
		// 3. Channel count divergence
		if pmt.Channels > 0 && probe.Channels > 0 && pmt.Channels != probe.Channels {
			conflicts = append(conflicts, Conflict{
				Field:      "channels",
				SourceA:    EvidencePMT,
				ValueA:     fmt.Sprintf("%d", pmt.Channels),
				SourceB:    EvidenceProbe,
				ValueB:     fmt.Sprintf("%d", probe.Channels),
				Resolution: "preferred_active_probe_measurement",
			})
		}

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
