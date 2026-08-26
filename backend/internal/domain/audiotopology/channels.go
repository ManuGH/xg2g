package audiotopology

// Source precedence is a per-field question, not a ranking of sources.
//
// For the channel count the elementary stream wins, because it is the only place
// the number exists: a DVB descriptor declares a class, and above stereo the
// class does not name a number at all - a 5.1 service and a 7.1 service declare
// the same value. A source that reads the audio frames can say six; one that
// reads the tables cannot.
//
// That does not make the stream the better source for everything. Language,
// purpose and accessibility are carried by the descriptors and not by an audio
// frame header, so for those fields the declaration stays authoritative and the
// observation has nothing to say. ESTrackObservation holds only the fields the
// frames answer, which is what keeps that honest: there is no channel-count
// precedence leaking into a language decision, because there is no language in
// the observation to leak.
//
// Between the two sources that do read frames, the continuous one wins. Both
// ffprobe and shared ingest read the same syntax; ffprobe reads it once, at
// session start, and a service that moves from stereo to 5.1 for a film leaves
// that reading describing a programme that has ended.
var channelSourcePrecedence = []EvidenceSource{
	EvidenceObserved, // read from the frames, continuously
	EvidenceProbe,    // read from the frames, once, at session start
	EvidencePMT,      // declared by a descriptor, and above stereo only as a class
}

// resolveChannels picks the channel count and names the source it came from.
//
// A source that names no count is skipped rather than treated as zero: silence
// about the channel count is not a claim that there are none, and it must not
// displace a source that did answer.
func resolveChannels(
	pmt *PMTTrackObservation,
	probe *ProbeTrackObservation,
	es *ESTrackObservation,
) (int, EvidenceSource, bool) {
	for _, source := range channelSourcePrecedence {
		switch source {
		case EvidenceObserved:
			if es != nil && es.Channels > 0 {
				return es.Channels, EvidenceObserved, true
			}
		case EvidenceProbe:
			if probe != nil && probe.Channels > 0 {
				return probe.Channels, EvidenceProbe, true
			}
		case EvidencePMT:
			if pmt != nil && pmt.Channels > 0 {
				return pmt.Channels, EvidencePMT, true
			}
		}
	}
	return 0, "", false
}
