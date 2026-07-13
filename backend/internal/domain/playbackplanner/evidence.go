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
	Validity        int64  // How long this truth remains valid
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
	// Ensure set-like slices are sorted canonically before hashing
	e.ClientEvidence.normalize()
	e.HostSnapshot.normalize()

	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return fmt.Sprintf("%x", hash), nil
}

type SourceTruth struct {
	Container  string
	VideoCodec string
	AudioCodec string
	Width      int
	Height     int
	FPS        int
	Interlaced bool
}

type ClientEvidence struct {
	Family               string
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
	SupportsRange        bool
}

func (c *ClientEvidence) normalize() {
	sort.Strings(c.SupportedContainers)
	sort.Strings(c.SupportedVideoCodecs)
	sort.Strings(c.SupportedAudioCodecs)
	sort.Strings(c.SupportedEngines)
}

func (h *HostSnapshot) normalize() {
	sort.Strings(h.AvailableEngines)
}

type NetworkEvidence struct {
	DownlinkKbps      int
	RTTMillis         int
	InternetValidated bool
}

type HostSnapshot struct {
	PressureBand     string // "relaxed", "constrained", "critical"
	AvailableEngines []string
}

type OperatorPolicy struct {
	DisableTranscoding bool
	MaxGlobalBitrate   int
}
