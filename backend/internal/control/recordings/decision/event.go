package decision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

type Event struct {
	ServiceRef       string                                 `json:"serviceRef"`
	SubjectKind      string                                 `json:"subjectKind"`
	Origin           string                                 `json:"origin"`
	ClientFamily     string                                 `json:"clientFamily"`
	ClientCapsSource string                                 `json:"clientCapsSource,omitempty"`
	DeviceType       string                                 `json:"deviceType,omitempty"`
	RequestedIntent  string                                 `json:"requestedIntent,omitempty"`
	ResolvedIntent   string                                 `json:"resolvedIntent,omitempty"`
	Mode             Mode                                   `json:"mode"`
	Selected         SelectedFormats                        `json:"selected"`
	Reasons          []ReasonCode                           `json:"reasons"`
	TargetProfile    *playbackprofile.TargetPlaybackProfile `json:"targetProfile,omitempty"`
	BasisHash        string                                 `json:"basisHash"`
	TruthHash        string                                 `json:"truthHash"`
	OutputHash       string                                 `json:"outputHash"`
	HostPressureBand string                                 `json:"hostPressureBand,omitempty"`
	DecidedAt        time.Time                              `json:"decidedAt"`
}

type EventMetadata struct {
	ServiceRef       string
	SubjectKind      string
	Origin           string
	ClientFamily     string
	ClientCapsSource string
	DeviceType       string
	DecidedAt        time.Time
}

const (
	OriginRuntime = "runtime"
	OriginSweep   = "sweep"
)

type EventSink interface {
	Record(ctx context.Context, event Event) error
}

func BuildEvent(meta EventMetadata, input DecisionInput, dec *Decision) (Event, error) {
	if dec == nil {
		return Event{}, fmt.Errorf("decision event requires non-nil decision")
	}

	input = NormalizeInput(input)
	selected := normalizeSelected(dec.Selected)
	reasons := append([]ReasonCode(nil), dec.Reasons...)
	targetProfile := dec.TargetProfile
	if targetProfile != nil {
		copied := *targetProfile
		targetProfile = &copied
	}

	basisHash := strings.TrimSpace(dec.Trace.InputHash)
	if basisHash == "" {
		basisHash = input.ComputeHash()
	}
	decidedAt := meta.DecidedAt.UTC()
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}

	outputHash, err := computeDecisionOutputHash(dec.Mode, selected, reasons, targetProfile)
	if err != nil {
		return Event{}, fmt.Errorf("compute decision output hash: %w", err)
	}

	return Event{
		ServiceRef:       normalize.ServiceRef(meta.ServiceRef),
		SubjectKind:      normalize.Token(meta.SubjectKind),
		Origin:           normalizeEventOrigin(meta.Origin),
		ClientFamily:     normalizeTokenOrUnknown(meta.ClientFamily),
		ClientCapsSource: normalize.Token(meta.ClientCapsSource),
		DeviceType:       normalize.Token(firstNonEmpty(meta.DeviceType, input.Capabilities.DeviceType)),
		RequestedIntent:  string(playbackprofile.NormalizeRequestedIntent(string(input.RequestedIntent))),
		ResolvedIntent:   normalize.Token(dec.Trace.ResolvedIntent),
		Mode:             dec.Mode,
		Selected:         selected,
		Reasons:          reasons,
		TargetProfile:    targetProfile,
		BasisHash:        basisHash,
		TruthHash:        computeTruthHash(input.Source),
		OutputHash:       outputHash,
		HostPressureBand: string(playbackprofile.NormalizeHostPressureBand(dec.Trace.HostPressureBand)),
		DecidedAt:        decidedAt,
	}, nil
}

func (e Event) Normalized() Event {
	e.ServiceRef = normalize.ServiceRef(e.ServiceRef)
	e.SubjectKind = normalize.Token(e.SubjectKind)
	e.Origin = normalizeEventOrigin(e.Origin)
	e.ClientFamily = normalizeTokenOrUnknown(e.ClientFamily)
	e.ClientCapsSource = normalize.Token(e.ClientCapsSource)
	e.DeviceType = normalize.Token(e.DeviceType)
	e.RequestedIntent = string(playbackprofile.NormalizeRequestedIntent(e.RequestedIntent))
	e.ResolvedIntent = normalize.Token(e.ResolvedIntent)
	e.Selected = normalizeSelected(e.Selected)
	e.HostPressureBand = string(playbackprofile.NormalizeHostPressureBand(e.HostPressureBand))
	e.DecidedAt = e.DecidedAt.UTC()
	if e.DecidedAt.IsZero() {
		e.DecidedAt = time.Now().UTC()
	}
	return e
}

func (e Event) Valid() error {
	switch {
	case e.ServiceRef == "":
		return fmt.Errorf("decision event requires service ref")
	case e.SubjectKind == "":
		return fmt.Errorf("decision event requires subject kind")
	case e.Origin == "":
		return fmt.Errorf("decision event requires origin")
	case e.Mode == "":
		return fmt.Errorf("decision event requires mode")
	case strings.TrimSpace(e.BasisHash) == "":
		return fmt.Errorf("decision event requires basis hash")
	case strings.TrimSpace(e.OutputHash) == "":
		return fmt.Errorf("decision event requires output hash")
	case strings.TrimSpace(e.TruthHash) == "":
		return fmt.Errorf("decision event requires truth hash")
	}
	return nil
}

type canonicalDecisionOutput struct {
	Mode          string                                 `json:"mode"`
	Selected      SelectedFormats                        `json:"selected"`
	Reasons       []string                               `json:"reasons"`
	TargetProfile *playbackprofile.TargetPlaybackProfile `json:"targetProfile,omitempty"`
}

type canonicalTruth struct {
	Container   string  `json:"container"`
	VideoCodec  string  `json:"videoCodec"`
	AudioCodec  string  `json:"audioCodec"`
	BitrateKbps int     `json:"bitrateKbps"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	FPS         float64 `json:"fps"`
}

func computeDecisionOutputHash(mode Mode, selected SelectedFormats, reasons []ReasonCode, targetProfile *playbackprofile.TargetPlaybackProfile) (string, error) {
	serializedReasons := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		serializedReasons = append(serializedReasons, string(reason))
	}
	payload, err := json.Marshal(canonicalDecisionOutput{
		Mode:          string(mode),
		Selected:      normalizeSelected(selected),
		Reasons:       serializedReasons,
		TargetProfile: targetProfile,
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(payload), nil
}

func computeTruthHash(source Source) string {
	payload, _ := json.Marshal(canonicalTruth{
		Container:   robustNorm(source.Container),
		VideoCodec:  robustNorm(source.VideoCodec),
		AudioCodec:  robustNorm(source.AudioCodec),
		BitrateKbps: source.BitrateKbps,
		Width:       source.Width,
		Height:      source.Height,
		FPS:         source.FPS,
	})
	return sha256Hex(payload)
}

func normalizeSelected(selected SelectedFormats) SelectedFormats {
	return SelectedFormats{
		Container:  robustNorm(selected.Container),
		VideoCodec: robustNorm(selected.VideoCodec),
		AudioCodec: robustNorm(selected.AudioCodec),
	}
}

func normalizeTokenOrUnknown(value string) string {
	if normalized := normalize.Token(value); normalized != "" {
		return normalized
	}
	return "unknown"
}

func normalizeEventOrigin(value string) string {
	if normalized := normalize.Token(value); normalized != "" {
		return normalized
	}
	return OriginRuntime
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
