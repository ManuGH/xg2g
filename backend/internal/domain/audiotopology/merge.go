package audiotopology

import (
	"fmt"
	"hash/fnv"
	"sort"
	"time"
)

// BuildTopology constructs an evidence-backed AudioTopology for a service session.
func BuildTopology(
	serviceRef string,
	pmtTracks []PMTTrackObservation,
	probeTracks []ProbeTrackObservation,
	e2Tracks []Enigma2TrackObservation,
	now time.Time,
) AudioTopology {
	pmtMap := make(map[uint16]PMTTrackObservation, len(pmtTracks))
	for _, p := range pmtTracks {
		pmtMap[p.PID] = p
	}

	probeMap := make(map[uint16]ProbeTrackObservation, len(probeTracks))
	for _, pr := range probeTracks {
		probeMap[pr.PID] = pr
	}

	e2Map := make(map[uint16]Enigma2TrackObservation, len(e2Tracks))
	for _, e := range e2Tracks {
		e2Map[e.PID] = e
	}

	// 1. Determine Presence State and physical PID candidates
	pidSet := make(map[uint16]struct{})
	presence := PresenceVerified

	for pid := range pmtMap {
		pidSet[pid] = struct{}{}
	}
	for pid := range probeMap {
		pidSet[pid] = struct{}{}
	}

	if len(pidSet) == 0 {
		if len(e2Map) > 0 {
			// Provisional state: Receiver metadata exists, but transport stream is unverified
			presence = PresenceProvisional
			for pid := range e2Map {
				pidSet[pid] = struct{}{}
			}
		} else {
			return AudioTopology{
				ServiceRef: serviceRef,
				Presence:   PresenceEmpty,
				Tracks:     nil,
				CreatedAt:  now,
			}
		}
	}

	// Sort PIDs for canonical ordering
	sortedPIDs := make([]uint16, 0, len(pidSet))
	for pid := range pidSet {
		sortedPIDs = append(sortedPIDs, pid)
	}
	sort.Slice(sortedPIDs, func(i, j int) bool {
		return sortedPIDs[i] < sortedPIDs[j]
	})

	// Pre-pass: Find primary language on the service from defaults or first declared stream
	primaryLangCode := determineServicePrimaryLanguage(pmtTracks, probeTracks, e2Tracks)

	var tracks []AudioTrack

	for _, pid := range sortedPIDs {
		var pmtPtr *PMTTrackObservation
		if p, ok := pmtMap[pid]; ok {
			pmtPtr = &p
		}

		var probePtr *ProbeTrackObservation
		if pr, ok := probeMap[pid]; ok {
			probePtr = &pr
		}

		var e2Ptr *Enigma2TrackObservation
		if e, ok := e2Map[pid]; ok {
			e2Ptr = &e
		}

		track := buildTrack(pid, primaryLangCode, pmtPtr, probePtr, e2Ptr)
		tracks = append(tracks, track)
	}

	structuralRev := computeStructuralRevision(tracks)
	metadataRev := computeMetadataRevision(tracks)

	return AudioTopology{
		ServiceRef:         serviceRef,
		StructuralRevision: structuralRev,
		MetadataRevision:   metadataRev,
		TopologyRevision:   structuralRev,
		Presence:           presence,
		Tracks:             tracks,
		CreatedAt:          now,
	}
}

func determineServicePrimaryLanguage(
	pmtTracks []PMTTrackObservation,
	probeTracks []ProbeTrackObservation,
	e2Tracks []Enigma2TrackObservation,
) string {
	for _, p := range pmtTracks {
		if p.IsDefault && p.Language != "" {
			return NormalizeLanguage(p.Language).ISO639_2
		}
	}
	for _, pr := range probeTracks {
		if pr.DispositionBroadcastDefault && pr.Language != "" {
			return NormalizeLanguage(pr.Language).ISO639_2
		}
	}
	if len(pmtTracks) > 0 && pmtTracks[0].Language != "" {
		return NormalizeLanguage(pmtTracks[0].Language).ISO639_2
	}
	if len(probeTracks) > 0 && probeTracks[0].Language != "" {
		return NormalizeLanguage(probeTracks[0].Language).ISO639_2
	}
	return "deu" // Default fallback for DVB DACH services
}

func buildTrack(
	pid uint16,
	primaryLangCode string,
	pmt *PMTTrackObservation,
	probe *ProbeTrackObservation,
	e2 *Enigma2TrackObservation,
) AudioTrack {
	var evidence []Observation

	// 1. Resolve Codec
	codec := CodecUnknown
	if pmt != nil {
		if pmt.Codec != "" {
			codec = NormalizeCodec(pmt.Codec)
			evidence = append(evidence, Observation{Source: EvidencePMT, Field: "codec", Value: pmt.Codec})
		} else if pmt.StreamType != 0 {
			codec = CodecFromDVBStreamType(pmt.StreamType)
			evidence = append(evidence, Observation{Source: EvidencePMT, Field: "stream_type", Value: fmt.Sprintf("0x%02x", pmt.StreamType)})
		}
	}
	if probe != nil && probe.Codec != "" {
		probeCodec := NormalizeCodec(probe.Codec)
		if codec == CodecUnknown || probeCodec != CodecUnknown {
			codec = probeCodec
		}
		evidence = append(evidence, Observation{Source: EvidenceProbe, Field: "codec", Value: probe.Codec})
	}

	// 2. Resolve Language
	var rawLang string
	if pmt != nil && pmt.Language != "" {
		rawLang = pmt.Language
		evidence = append(evidence, Observation{Source: EvidencePMT, Field: "language", Value: pmt.Language})
	}
	if rawLang == "" && probe != nil && probe.Language != "" {
		rawLang = probe.Language
		evidence = append(evidence, Observation{Source: EvidenceProbe, Field: "language", Value: probe.Language})
	}
	lang := NormalizeLanguage(rawLang)

	// 3. Resolve Channels & Bitrates
	channels := 2
	sampleRate := 48000
	bitrateKbps := 0

	if pmt != nil {
		if pmt.Channels > 0 {
			channels = pmt.Channels
			evidence = append(evidence, Observation{Source: EvidencePMT, Field: "channels", Value: fmt.Sprintf("%d", pmt.Channels)})
		}
		if pmt.SampleRate > 0 {
			sampleRate = pmt.SampleRate
		}
		if pmt.BitrateKbps > 0 {
			bitrateKbps = pmt.BitrateKbps
		}
	}

	if probe != nil {
		if probe.Channels > 0 {
			channels = probe.Channels
			evidence = append(evidence, Observation{Source: EvidenceProbe, Field: "channels", Value: fmt.Sprintf("%d", probe.Channels)})
		}
		if probe.BitrateKbps > 0 {
			bitrateKbps = probe.BitrateKbps
		}
	}

	// 4. Resolve Accessibility & Purpose
	visualImpaired := false
	hearingImpaired := false
	cleanEffects := false
	broadcastDefault := false

	if pmt != nil {
		visualImpaired = pmt.VisualImpaired || (pmt.AudioType == 0x03)
		hearingImpaired = pmt.HearingImpaired || (pmt.AudioType == 0x02)
		cleanEffects = pmt.CleanEffects || (pmt.AudioType == 0x01)
		broadcastDefault = pmt.IsDefault
		if pmt.VisualImpaired {
			evidence = append(evidence, Observation{Source: EvidencePMT, Field: "visual_impaired", Value: "true"})
		}
		if pmt.HearingImpaired {
			evidence = append(evidence, Observation{Source: EvidencePMT, Field: "hearing_impaired", Value: "true"})
		}
	}

	if probe != nil {
		if probe.DispositionVisualImpaired || probe.DispositionDescriptions {
			visualImpaired = true
			evidence = append(evidence, Observation{Source: EvidenceProbe, Field: "disposition_visual_impaired", Value: "true"})
		}
		if probe.DispositionHearingImpaired {
			hearingImpaired = true
			evidence = append(evidence, Observation{Source: EvidenceProbe, Field: "disposition_hearing_impaired", Value: "true"})
		}
		if probe.DispositionCleanEffects {
			cleanEffects = true
			evidence = append(evidence, Observation{Source: EvidenceProbe, Field: "disposition_clean_effects", Value: "true"})
		}
		if probe.DispositionBroadcastDefault {
			broadcastDefault = true
		}
	}

	var e2Desc string
	receiverSelected := false
	if e2 != nil {
		e2Desc = e2.Description
		receiverSelected = e2.Active
		evidence = append(evidence, Observation{Source: EvidenceEnigma2, Field: "description", Value: e2.Description})
		if e2.Active {
			evidence = append(evidence, Observation{Source: EvidenceEnigma2, Field: "active", Value: "true"})
		}
	}

	acc := ClassifyAccessibility(visualImpaired, hearingImpaired, e2Desc)

	// IsPrimary is based on broadcast default, receiver selection, or matching primary service language
	isPrimary := broadcastDefault || receiverSelected || (lang.ISO639_2 == primaryLangCode)
	purpose, confidence := ClassifyPurpose(lang, e2Desc, cleanEffects, isPrimary)
	label := BuildTrackLabel(lang, codec, channels, purpose, acc, e2Desc)
	conflicts := DetectConflicts(pmt, probe, e2)

	return AudioTrack{
		ID:               fmt.Sprintf("audio-%d", pid),
		PID:              pid,
		Codec:            codec,
		Channels:         channels,
		SampleRate:       sampleRate,
		BitrateKbps:      bitrateKbps,
		Language:         lang,
		Purpose:          purpose,
		Accessibility:    acc,
		BroadcastDefault: broadcastDefault,
		ReceiverSelected: receiverSelected,
		Label:            label,
		Confidence:       confidence,
		Evidence:         evidence,
		Conflicts:        conflicts,
	}
}

// computeStructuralRevision computes a hash over stream structure (PIDs, codecs, channels, language, purpose, accessibility).
func computeStructuralRevision(tracks []AudioTrack) uint64 {
	hasher := fnv.New64a()
	for _, t := range tracks {
		_, _ = fmt.Fprintf(hasher, "%d:%s:%d:%s:%s:%t:%t:%t|",
			t.PID,
			t.Codec,
			t.Channels,
			t.Language.Code,
			t.Purpose,
			t.Accessibility.AudioDescription,
			t.Accessibility.HearingImpaired,
			t.Accessibility.ClearDialogue,
		)
	}
	return hasher.Sum64()
}

// computeMetadataRevision computes a hash over user-facing metadata, selections, and evidence states.
func computeMetadataRevision(tracks []AudioTrack) uint64 {
	hasher := fnv.New64a()
	for _, t := range tracks {
		_, _ = fmt.Fprintf(hasher, "%d:%s:%t:%t:%s:%d:%d|",
			t.PID,
			t.Label,
			t.BroadcastDefault,
			t.ReceiverSelected,
			t.Confidence,
			len(t.Evidence),
			len(t.Conflicts),
		)
	}
	return hasher.Sum64()
}
