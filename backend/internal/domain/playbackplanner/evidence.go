package playbackplanner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// PlaybackEvidence represents all inputs required to make a playback decision.
type PlaybackEvidence struct {
	// EvaluatedAt represents the Unix timestamp (milliseconds) when this evidence was gathered.
	// Used for all freshness calculations to ensure Plan() remains pure.
	EvaluatedAt int64

	Scope           string // e.g., "live", "recording"
	RequestedIntent string // e.g., "stream_start"
	SourceIdentity  string // e.g., "1:0:1:...", recording ID
	Provenance      string // origin of the truth (e.g., "scan", "media_file")
	Confidence      string // e.g., "ok", "partial", "stale"
	ObservedAt      int64  // When the source truth was actually observed
	ValidUntil      int64  // Unix milliseconds until this truth expires
	NetworkCaptureTime int64 // When network conditions were captured
	PolicyVersion   string

	// SourceTruth contains information about the media source.
	SourceTruth SourceTruth

	// ClientEvidence contains information about the requesting client's capabilities.
	ClientEvidence ClientEvidence

	// NetworkEvidence contains information about the network conditions.
	NetworkEvidence NetworkEvidence

	// HostSnapshot contains information about the server's current hardware and capacity.
	HostSnapshot HostSnapshot

	// OperatorPolicy contains business rules and configuration limits.
	OperatorPolicy OperatorPolicy
}

// Hash returns a deterministic hash of the evidence.
func (e PlaybackEvidence) Hash() (string, error) {
	// e is a value copy, but its slices point to the original arrays.
	// We must deep-clone, sort, and deduplicate to ensure pure hashing.
	e.ClientEvidence = e.ClientEvidence.cloneNormalized()
	e.HostSnapshot = e.HostSnapshot.cloneNormalized()

	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return fmt.Sprintf("%x", hash), nil
}

type SourceTruth struct {
	Container         string
	VideoCodec        string
	AudioCodec        string
	Width             int
	Height            int
	FPS               int
	Interlaced        bool
	BitrateKbps       int
	BitrateConfidence string
}

type ClientEvidence struct {
	Family               string
	DeviceType           string
	CapabilityVersion    string
	AllowTranscode       bool
	SupportedContainers  []string
	SupportedVideoCodecs []string
	SupportedAudioCodecs []string
	MaxVideoWidth        int
	MaxVideoHeight       int
	MaxVideoFPS          int
	
	// Added packaging and engine evidence
	PreferredEngine      string
	SupportedEngines     []string
	SupportsHls          bool
	SupportsRange        *bool // Tri-state: true, false, nil (unknown)
}

func cloneDeduplicateSort(input []string) []string {
	if input == nil {
		return nil
	}
	set := make(map[string]struct{}, len(input))
	for _, v := range input {
		set[v] = struct{}{}
	}
	var res []string
	for v := range set {
		res = append(res, v)
	}
	sort.Strings(res)
	return res
}

func (c ClientEvidence) cloneNormalized() ClientEvidence {
	c.SupportedContainers = cloneDeduplicateSort(c.SupportedContainers)
	c.SupportedVideoCodecs = cloneDeduplicateSort(c.SupportedVideoCodecs)
	c.SupportedAudioCodecs = cloneDeduplicateSort(c.SupportedAudioCodecs)
	c.SupportedEngines = cloneDeduplicateSort(c.SupportedEngines)
	return c
}

func (h HostSnapshot) cloneNormalized() HostSnapshot {
	h.AvailableEngines = cloneDeduplicateSort(h.AvailableEngines)
	return h
}

type NetworkEvidence struct {
	DownlinkKbps      int
	RTTMillis         int
	InternetValidated bool
}

type HostSnapshot struct {
	PressureBand     string // "relaxed", "constrained", "critical"
	AvailableEngines []string
	PerformanceClass string
	BenchmarkClass   string
}

type OperatorPolicy struct {
	ForceIntent        string // "copy", "remux", "transcode"
	MaxQualityRung     string // Highest allowed bit-rate tier
	DisableTranscoding bool   // Operator-level switch to block transcoding (e.g., peak load)
	MaxGlobalBitrate   int    // Global bandwidth cap per session (kbps)
	StrictFreshness    bool   // If true, stale evidence results in hard denial instead of fallback
}
