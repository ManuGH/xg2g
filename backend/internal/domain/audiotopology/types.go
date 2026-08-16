package audiotopology

import (
	"time"
)

// EvidenceSource denotes where a particular stream observation originated.
type EvidenceSource string

const (
	EvidencePMT     EvidenceSource = "pmt"
	EvidenceEnigma2 EvidenceSource = "enigma2"
	EvidenceProbe   EvidenceSource = "probe"
)

// PresenceState indicates the degree of physical verification for the topology.
type PresenceState string

const (
	PresenceVerified    PresenceState = "verified"    // Backed by actual PMT or elementary stream probe
	PresenceProvisional PresenceState = "provisional" // Derived from receiver metadata prior to stream verification
	PresenceEmpty       PresenceState = "empty"       // No tracks present or discovered
)

// Confidence expresses the epistemic reliability of a derived track property.
type Confidence string

const (
	ConfidenceExplicit  Confidence = "explicit"
	ConfidenceHigh      Confidence = "high"
	ConfidenceMedium    Confidence = "medium"
	ConfidenceLow       Confidence = "low"
	ConfidenceHeuristic Confidence = "heuristic"
)

// AudioCodec canonicalizes common broadcast audio stream codecs.
type AudioCodec string

const (
	CodecMP2     AudioCodec = "mp2"
	CodecAC3     AudioCodec = "ac3"
	CodecEAC3    AudioCodec = "eac3"
	CodecAAC     AudioCodec = "aac"
	CodecDTS     AudioCodec = "dts"
	CodecPCM     AudioCodec = "pcm"
	CodecUnknown AudioCodec = "unknown"
)

// AudioPurpose describes the primary semantic intent of the audio track.
type AudioPurpose string

const (
	AudioPurposeMain       AudioPurpose = "main"
	AudioPurposeAlternate  AudioPurpose = "alternate"
	AudioPurposeCommentary AudioPurpose = "commentary"
)

// AudioAccessibility captures barrier-free accessibility dimensions.
type AudioAccessibility struct {
	AudioDescription bool `json:"audioDescription"`
	HearingImpaired  bool `json:"hearingImpaired"`
	ClearDialogue    bool `json:"clearDialogue"`
}

// LanguageInfo represents normalized ISO-639 language descriptors.
type LanguageInfo struct {
	Code        string `json:"code"`
	ISO639_2    string `json:"iso639_2"`
	ISO639_1    string `json:"iso639_1"`
	IsOriginal  bool   `json:"isOriginal"`
	IsUndefined bool   `json:"isUndefined"`
}

// Observation records an atomic factual observation from a verified source.
type Observation struct {
	Source EvidenceSource `json:"source"`
	Field  string         `json:"field"`
	Value  string         `json:"value"`
}

// Conflict records an irreconcilable contradiction between two evidence sources.
type Conflict struct {
	Field      string         `json:"field"`
	SourceA    EvidenceSource `json:"sourceA"`
	ValueA     string         `json:"valueA"`
	SourceB    EvidenceSource `json:"sourceB"`
	ValueB     string         `json:"valueB"`
	Resolution string         `json:"resolution,omitempty"`
}

// AudioTrack represents a fully resolved, evidence-backed audio track.
type AudioTrack struct {
	ID               string             `json:"id"`
	PID              uint16             `json:"pid"`
	Codec            AudioCodec         `json:"codec"`
	Channels         int                `json:"channels"`
	SampleRate       int                `json:"sampleRate,omitempty"`
	BitrateKbps      int                `json:"bitrateKbps,omitempty"`
	Language         LanguageInfo       `json:"language"`
	Purpose          AudioPurpose       `json:"purpose"`
	Accessibility    AudioAccessibility `json:"accessibility"`
	BroadcastDefault bool               `json:"broadcastDefault"`
	ReceiverSelected bool               `json:"receiverSelected"`
	Label            string             `json:"label"`
	Confidence       Confidence         `json:"confidence"`
	Evidence         []Observation      `json:"evidence"`
	Conflicts        []Conflict         `json:"conflicts,omitempty"`
}

// AudioTopology represents a complete, immutable snapshot of the audio layout
// for a service session at a specific revision.
type AudioTopology struct {
	ServiceRef         string        `json:"serviceRef"`
	StructuralRevision uint64        `json:"structuralRevision"` // Changes when streamset, PIDs, codecs, channels change (HLS epoch rollover)
	MetadataRevision   uint64        `json:"metadataRevision"`   // Changes when labels, selection, or confidence change (in-place manifest refresh)
	TopologyRevision   uint64        `json:"topologyRevision"`   // Legacy alias matching StructuralRevision
	Presence           PresenceState `json:"presence"`
	Tracks             []AudioTrack  `json:"tracks"`
	CreatedAt          time.Time     `json:"createdAt"`
}

// PMTTrackObservation represents raw elementary stream data parsed from a DVB PMT.
type PMTTrackObservation struct {
	PID             uint16 `json:"pid"`
	StreamType      uint8  `json:"streamType"`
	Codec           string `json:"codec"`
	Language        string `json:"language"`
	Channels        int    `json:"channels"`
	SampleRate      int    `json:"sampleRate,omitempty"`
	BitrateKbps     int    `json:"bitrateKbps,omitempty"`
	AudioType       uint8  `json:"audioType,omitempty"` // ETSI EN 300 468 audio_type
	VisualImpaired  bool   `json:"visualImpaired"`
	HearingImpaired bool   `json:"hearingImpaired"`
	CleanEffects    bool   `json:"cleanEffects"`
	IsDefault       bool   `json:"isDefault"`
}

// Enigma2TrackObservation represents metadata from OpenWebIF /web/getaudiotracks.
type Enigma2TrackObservation struct {
	TrackID     int    `json:"trackId"`
	PID         uint16 `json:"pid"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

// ProbeTrackObservation represents elementary stream data parsed by an active session probe.
type ProbeTrackObservation struct {
	StreamIndex                 int    `json:"streamIndex"`
	PID                         uint16 `json:"pid"`
	Codec                       string `json:"codec"`
	Channels                    int    `json:"channels"`
	ChannelLayout               string `json:"channelLayout,omitempty"`
	Language                    string `json:"language"`
	BitrateKbps                 int    `json:"bitrateKbps,omitempty"`
	DispositionVisualImpaired   bool   `json:"dispositionVisualImpaired"`
	DispositionHearingImpaired  bool   `json:"dispositionHearingImpaired"`
	DispositionCleanEffects     bool   `json:"dispositionCleanEffects"`
	DispositionDescriptions     bool   `json:"dispositionDescriptions"`
	DispositionBroadcastDefault bool   `json:"dispositionBroadcastDefault"`
}
