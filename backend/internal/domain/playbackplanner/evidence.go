package playbackplanner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// PlaybackEvidence represents all inputs required to make a playback decision.
type PlaybackEvidence struct {
	// EvaluatedAt represents the Unix timestamp (milliseconds) when this evidence was gathered.
	// Used for all freshness calculations to ensure Plan() remains pure.
	EvaluatedAt int64

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
