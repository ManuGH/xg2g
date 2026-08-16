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

	visualImpaired := false
	if pmt != nil && (pmt.VisualImpaired || pmt.AudioType == 0x03) {
		visualImpaired = true
	}
	if probe != nil && (probe.DispositionVisualImpaired || probe.DispositionDescriptions) {
		visualImpaired = true
	}

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

func streamNormIsGeneric(code string) bool {
	return code == "und" || code == "mis" || code == "mul" || code == ""
}
