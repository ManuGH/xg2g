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
	// Index observations by PID
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

	// 1. Gather all physically present PIDs (Presence Truth from PMT or Probe)
	pidSet := make(map[uint16]struct{})
	for pid := range pmtMap {
		pidSet[pid] = struct{}{}
	}
	for pid := range probeMap {
		pidSet[pid] = struct{}{}
	}

	// Fallback: If no PMT/Probe exists yet, permit Enigma2 tracks (graceful start)
	if len(pidSet) == 0 {
		for pid := range e2Map {
			pidSet[pid] = struct{}{}
		}
	}

	// Sort PIDs for determinism
	sortedPIDs := make([]uint16, 0, len(pidSet))
	for pid := range pidSet {
		sortedPIDs = append(sortedPIDs, pid)
	}
	sort.Slice(sortedPIDs, func(i, j int) bool {
		return sortedPIDs[i] < sortedPIDs[j]
	})

	var tracks []AudioTrack

	for i, pid := range sortedPIDs {
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

		track := buildTrack(i, pid, pmtPtr, probePtr, e2Ptr)
		tracks = append(tracks, track)
	}

	revision := computeTopologyRevision(tracks)

	return AudioTopology{
		ServiceRef:       serviceRef,
		TopologyRevision: revision,
		Tracks:           tracks,
		CreatedAt:        now,
	}
}

func buildTrack(
	index int,
	pid uint16,
	pmt *PMTTrackObservation,
	probe *ProbeTrackObservation,
	e2 *Enigma2TrackObservation,
) AudioTrack {
	var evidence []Observation

	// 1. Resolve Codec
	var codec AudioCodec = CodecUnknown
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

	// 3. Resolve Channels & Audio Properties
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
	isPrimaryLang := (index == 0)
	purpose, confidence := ClassifyPurpose(lang, e2Desc, cleanEffects, isPrimaryLang)
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

// computeTopologyRevision calculates a deterministic 64-bit hash over the track topology.
func computeTopologyRevision(tracks []AudioTrack) uint64 {
	hasher := fnv.New64a()
	for _, t := range tracks {
		fmt.Fprintf(hasher, "%d:%s:%d:%s:%s:%t:%t:%t|",
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
