package capreg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

type Store interface {
	RememberHost(ctx context.Context, snapshot HostSnapshot) error
	RememberDevice(ctx context.Context, snapshot DeviceSnapshot) error
	RememberSource(ctx context.Context, snapshot SourceSnapshot) error
	LookupCapabilities(ctx context.Context, identity DeviceIdentity) (capabilities.PlaybackCapabilities, bool, error)
	LookupDecisionObservation(ctx context.Context, requestID string) (PlaybackObservation, bool, error)
	RecordObservation(ctx context.Context, observation PlaybackObservation) error
}

type DeviceIdentity struct {
	ClientFamily     string
	ClientCapsSource string
	DeviceType       string
	DeviceContext    *capabilities.DeviceContext
}

type DeviceSnapshot struct {
	Identity     DeviceIdentity
	Capabilities capabilities.PlaybackCapabilities
	Network      *capabilities.NetworkContext
	UpdatedAt    time.Time
}

type HostIdentity struct {
	Hostname     string
	OSName       string
	OSVersion    string
	Architecture string
}

type EncoderCapability struct {
	Codec          string `json:"codec"`
	Verified       bool   `json:"verified"`
	AutoEligible   bool   `json:"autoEligible"`
	ProbeElapsedMS int64  `json:"probeElapsedMs,omitempty"`
}

type HostSnapshot struct {
	Identity            HostIdentity
	Runtime             playbackprofile.HostRuntimeSnapshot
	EncoderCapabilities []EncoderCapability
	UpdatedAt           time.Time
}

type ReceiverContext struct {
	Platform            string `json:"platform,omitempty"`
	Brand               string `json:"brand,omitempty"`
	Model               string `json:"model,omitempty"`
	OSName              string `json:"osName,omitempty"`
	OSVersion           string `json:"osVersion,omitempty"`
	KernelVersion       string `json:"kernelVersion,omitempty"`
	EnigmaVersion       string `json:"enigmaVersion,omitempty"`
	WebInterfaceVersion string `json:"webInterfaceVersion,omitempty"`
}

type SourceSnapshot struct {
	SubjectKind     string
	Origin          string
	Container       string
	VideoCodec      string
	AudioCodec      string
	Width           int
	Height          int
	FPS             float64
	Interlaced      bool
	ProblemFlags    []string
	ReceiverContext *ReceiverContext
	UpdatedAt       time.Time
}

func (s SourceSnapshot) Fingerprint() string {
	canonical := canonicalSourceSnapshot(s)
	if canonical.SubjectKind == "" && canonical.Container == "" && canonical.VideoCodec == "" && canonical.AudioCodec == "" {
		return ""
	}
	return sha256JSON(map[string]any{
		"subjectKind":  canonical.SubjectKind,
		"origin":       canonical.Origin,
		"container":    canonical.Container,
		"videoCodec":   canonical.VideoCodec,
		"audioCodec":   canonical.AudioCodec,
		"width":        canonical.Width,
		"height":       canonical.Height,
		"fps":          canonical.FPS,
		"interlaced":   canonical.Interlaced,
		"problemFlags": canonical.ProblemFlags,
		"receiver":     canonical.ReceiverContext,
	})
}

type PlaybackObservation struct {
	ObservedAt         time.Time
	RequestID          string
	ObservationKind    string
	Outcome            string
	SessionID          string
	SourceRef          string
	SourceFingerprint  string
	SubjectKind        string
	RequestedIntent    string
	ResolvedIntent     string
	Mode               string
	SelectedContainer  string
	SelectedVideoCodec string
	SelectedAudioCodec string
	SourceWidth        int
	SourceHeight       int
	SourceFPS          float64
	HostFingerprint    string
	DeviceFingerprint  string
	ClientCapsHash     string
	Network            *capabilities.NetworkContext
	FeedbackEvent      string
	FeedbackCode       int
	FeedbackMessage    string
}

func NewStore(backend, storagePath string) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "sqlite":
		return NewSqliteStore(filepath.Join(storagePath, "capability_registry.sqlite"))
	case "memory":
		return NewMemoryStore(), nil
	default:
		return nil, fmt.Errorf("unknown capability registry backend: %s (supported: sqlite, memory)", backend)
	}
}

type MemoryStore struct {
	mu           sync.Mutex
	hosts        map[string]HostSnapshot
	devices      map[string]DeviceSnapshot
	sources      map[string]SourceSnapshot
	observations []PlaybackObservation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		hosts:        make(map[string]HostSnapshot),
		devices:      make(map[string]DeviceSnapshot),
		sources:      make(map[string]SourceSnapshot),
		observations: make([]PlaybackObservation, 0, 32),
	}
}

func (s *MemoryStore) RememberHost(_ context.Context, snapshot HostSnapshot) error {
	snapshot = canonicalHostSnapshot(snapshot)
	fingerprint := snapshot.Identity.Fingerprint()
	if fingerprint == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[fingerprint] = snapshot
	return nil
}

func (s *MemoryStore) RememberDevice(_ context.Context, snapshot DeviceSnapshot) error {
	snapshot = canonicalDeviceSnapshot(snapshot)
	fingerprint := snapshot.Identity.Fingerprint()
	if fingerprint == "" || snapshot.Capabilities.CapabilitiesVersion == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[fingerprint] = snapshot
	return nil
}

func (s *MemoryStore) RememberSource(_ context.Context, snapshot SourceSnapshot) error {
	snapshot = canonicalSourceSnapshot(snapshot)
	fingerprint := snapshot.Fingerprint()
	if fingerprint == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sources[fingerprint] = snapshot
	return nil
}

func (s *MemoryStore) LookupCapabilities(_ context.Context, identity DeviceIdentity) (capabilities.PlaybackCapabilities, bool, error) {
	fingerprint := identity.Fingerprint()
	if fingerprint == "" {
		return capabilities.PlaybackCapabilities{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.devices[fingerprint]
	if !ok {
		return capabilities.PlaybackCapabilities{}, false, nil
	}
	return snapshot.Capabilities, true, nil
}

func (s *MemoryStore) LookupDecisionObservation(_ context.Context, requestID string) (PlaybackObservation, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return PlaybackObservation{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := len(s.observations) - 1; idx >= 0; idx-- {
		observation := s.observations[idx]
		if observation.RequestID != requestID || observation.ObservationKind != "decision" {
			continue
		}
		return observation, true, nil
	}
	return PlaybackObservation{}, false, nil
}

func (s *MemoryStore) RecordObservation(_ context.Context, observation PlaybackObservation) error {
	observation = canonicalObservation(observation)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observations = append(s.observations, observation)
	return nil
}

func canonicalDeviceIdentity(in DeviceIdentity) DeviceIdentity {
	out := in
	out.ClientFamily = normalizeToken(out.ClientFamily)
	out.ClientCapsSource = normalizeToken(out.ClientCapsSource)
	out.DeviceType = normalizeToken(out.DeviceType)
	out.DeviceContext = canonicalDeviceContext(out.DeviceContext)
	return out
}

func (i DeviceIdentity) Fingerprint() string {
	canonical := canonicalDeviceIdentity(i)
	if canonical.ClientFamily == "" && canonical.ClientCapsSource == "" && canonical.DeviceType == "" && canonical.DeviceContext == nil {
		return ""
	}
	return sha256JSON(map[string]any{
		"clientFamily":     canonical.ClientFamily,
		"clientCapsSource": canonical.ClientCapsSource,
		"deviceType":       canonical.DeviceType,
		"deviceContext":    canonical.DeviceContext,
	})
}

func canonicalHostIdentity(in HostIdentity) HostIdentity {
	out := in
	out.Hostname = normalizeToken(out.Hostname)
	out.OSName = normalizeToken(out.OSName)
	out.OSVersion = strings.TrimSpace(strings.ToLower(out.OSVersion))
	out.Architecture = normalizeToken(out.Architecture)
	return out
}

func (i HostIdentity) Fingerprint() string {
	canonical := canonicalHostIdentity(i)
	if canonical.Hostname == "" && canonical.OSName == "" && canonical.OSVersion == "" && canonical.Architecture == "" {
		return ""
	}
	return sha256JSON(map[string]any{
		"hostname":     canonical.Hostname,
		"osName":       canonical.OSName,
		"osVersion":    canonical.OSVersion,
		"architecture": canonical.Architecture,
	})
}

func canonicalDeviceSnapshot(in DeviceSnapshot) DeviceSnapshot {
	out := in
	out.Identity = canonicalDeviceIdentity(out.Identity)
	out.Capabilities = capabilities.CanonicalizeCapabilities(out.Capabilities)
	out.Network = canonicalNetworkContext(out.Network)
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	return out
}

func canonicalHostSnapshot(in HostSnapshot) HostSnapshot {
	out := in
	out.Identity = canonicalHostIdentity(out.Identity)
	out.Runtime = playbackprofile.CanonicalizeHostRuntime(out.Runtime)
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	if out.EncoderCapabilities == nil {
		out.EncoderCapabilities = []EncoderCapability{}
	}
	for idx := range out.EncoderCapabilities {
		out.EncoderCapabilities[idx].Codec = normalizeToken(out.EncoderCapabilities[idx].Codec)
		if out.EncoderCapabilities[idx].ProbeElapsedMS < 0 {
			out.EncoderCapabilities[idx].ProbeElapsedMS = 0
		}
	}
	return out
}

func canonicalSourceSnapshot(in SourceSnapshot) SourceSnapshot {
	out := in
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.Origin = normalizeToken(out.Origin)
	out.Container = normalizeSourceContainer(out.Container)
	out.VideoCodec = normalizeToken(out.VideoCodec)
	out.AudioCodec = normalizeToken(out.AudioCodec)
	if out.Width < 0 {
		out.Width = 0
	}
	if out.Height < 0 {
		out.Height = 0
	}
	if out.FPS < 0 {
		out.FPS = 0
	}
	out.ProblemFlags = canonicalStringSlice(out.ProblemFlags)
	out.ReceiverContext = canonicalReceiverContext(out.ReceiverContext)
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	return out
}

func HashCapabilitiesSnapshot(in capabilities.PlaybackCapabilities) string {
	canonical := capabilities.CanonicalizeCapabilities(in)
	canonical.NetworkContext = nil
	return sha256JSON(canonical)
}

func canonicalObservation(in PlaybackObservation) PlaybackObservation {
	out := in
	if out.ObservedAt.IsZero() {
		out.ObservedAt = time.Now().UTC()
	} else {
		out.ObservedAt = out.ObservedAt.UTC()
	}
	out.RequestID = strings.TrimSpace(out.RequestID)
	out.ObservationKind = normalizeToken(out.ObservationKind)
	if out.ObservationKind == "" {
		out.ObservationKind = "decision"
	}
	out.Outcome = normalizeToken(out.Outcome)
	if out.Outcome == "" {
		out.Outcome = "predicted"
	}
	out.SessionID = strings.TrimSpace(out.SessionID)
	out.SourceRef = strings.TrimSpace(out.SourceRef)
	out.SourceFingerprint = strings.TrimSpace(out.SourceFingerprint)
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.RequestedIntent = normalizeToken(out.RequestedIntent)
	out.ResolvedIntent = normalizeToken(out.ResolvedIntent)
	out.Mode = normalizeToken(out.Mode)
	out.SelectedContainer = normalizeToken(out.SelectedContainer)
	out.SelectedVideoCodec = normalizeToken(out.SelectedVideoCodec)
	out.SelectedAudioCodec = normalizeToken(out.SelectedAudioCodec)
	if out.SourceWidth < 0 {
		out.SourceWidth = 0
	}
	if out.SourceHeight < 0 {
		out.SourceHeight = 0
	}
	if out.SourceFPS < 0 {
		out.SourceFPS = 0
	}
	out.HostFingerprint = strings.TrimSpace(out.HostFingerprint)
	out.DeviceFingerprint = strings.TrimSpace(out.DeviceFingerprint)
	out.ClientCapsHash = strings.TrimSpace(out.ClientCapsHash)
	out.Network = canonicalNetworkContext(out.Network)
	out.FeedbackEvent = normalizeToken(out.FeedbackEvent)
	if out.FeedbackCode < 0 {
		out.FeedbackCode = 0
	}
	out.FeedbackMessage = strings.TrimSpace(out.FeedbackMessage)
	return out
}

func canonicalDeviceContext(in *capabilities.DeviceContext) *capabilities.DeviceContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Brand = normalizeToken(out.Brand)
	out.Product = normalizeToken(out.Product)
	out.Device = normalizeToken(out.Device)
	out.Platform = normalizeToken(out.Platform)
	out.Manufacturer = normalizeToken(out.Manufacturer)
	out.Model = normalizeToken(out.Model)
	out.OSName = normalizeToken(out.OSName)
	out.OSVersion = strings.TrimSpace(strings.ToLower(out.OSVersion))
	if out.SDKInt < 0 {
		out.SDKInt = 0
	}
	if out.Brand == "" && out.Product == "" && out.Device == "" && out.Platform == "" && out.Manufacturer == "" && out.Model == "" && out.OSName == "" && out.OSVersion == "" && out.SDKInt == 0 {
		return nil
	}
	return &out
}

func canonicalReceiverContext(in *ReceiverContext) *ReceiverContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Platform = normalizeToken(out.Platform)
	out.Brand = normalizeToken(out.Brand)
	out.Model = normalizeToken(out.Model)
	out.OSName = normalizeToken(out.OSName)
	out.OSVersion = strings.TrimSpace(out.OSVersion)
	out.KernelVersion = strings.TrimSpace(out.KernelVersion)
	out.EnigmaVersion = strings.TrimSpace(out.EnigmaVersion)
	out.WebInterfaceVersion = strings.TrimSpace(out.WebInterfaceVersion)
	if out.Platform == "" && out.Brand == "" && out.Model == "" && out.OSName == "" && out.OSVersion == "" && out.KernelVersion == "" && out.EnigmaVersion == "" && out.WebInterfaceVersion == "" {
		return nil
	}
	return &out
}

func canonicalNetworkContext(in *capabilities.NetworkContext) *capabilities.NetworkContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Kind = normalizeToken(out.Kind)
	if out.DownlinkKbps < 0 {
		out.DownlinkKbps = 0
	}
	if out.Kind == "" && out.DownlinkKbps == 0 && out.Metered == nil && out.InternetValidated == nil {
		return nil
	}
	return &out
}

func normalizeToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeSourceContainer(s string) string {
	switch normalizeToken(s) {
	case "mpegts":
		return "ts"
	default:
		return normalizeToken(s)
	}
}

func canonicalStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		token := normalizeToken(raw)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func sha256JSON(v any) string {
	payload, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
