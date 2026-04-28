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
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
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

type decisionFingerprintInput struct {
	Version        string                          `json:"version"`
	HostClass      string                          `json:"hostClass,omitempty"`
	BenchmarkClass string                          `json:"benchmarkClass,omitempty"`
	ProfileKeys    []decisionFingerprintProfileKey `json:"profileKeys,omitempty"`
	OSName         string                          `json:"osName,omitempty"`
	OSVersion      string                          `json:"osVersion,omitempty"`
	Architecture   string                          `json:"architecture,omitempty"`
	EncoderKeys    []decisionFingerprintEncoderKey `json:"encoderKeys,omitempty"`
}

type decisionFingerprintEncoderKey struct {
	Codec string `json:"codec"`
}

type decisionFingerprintProfileKey struct {
	ProfileID string `json:"profileId"`
	Class     string `json:"class,omitempty"`
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
	SubjectKind       string
	Origin            string
	Container         string
	VideoCodec        string
	AudioCodec        string
	BitrateConfidence string
	BitrateBucket     string
	Width             int
	Height            int
	FPS               float64
	SignalFPS         float64
	Interlaced        bool
	ProblemFlags      []string
	ReceiverContext   *ReceiverContext
	UpdatedAt         time.Time
}

func (s SourceSnapshot) Fingerprint() string {
	canonical := canonicalSourceSnapshot(s)
	if canonical.SubjectKind == "" && canonical.Container == "" && canonical.VideoCodec == "" && canonical.AudioCodec == "" {
		return ""
	}
	return sha256JSON(map[string]any{
		"subjectKind":       canonical.SubjectKind,
		"origin":            canonical.Origin,
		"container":         canonical.Container,
		"videoCodec":        canonical.VideoCodec,
		"audioCodec":        canonical.AudioCodec,
		"bitrateConfidence": canonical.BitrateConfidence,
		"bitrateBucket":     canonical.BitrateBucket,
		"width":             canonical.Width,
		"height":            canonical.Height,
		"fps":               canonical.FPS,
		"signalFps":         canonical.SignalFPS,
		"interlaced":        canonical.Interlaced,
		"problemFlags":      canonical.ProblemFlags,
		"receiver":          canonical.ReceiverContext,
	})
}

func (s HostSnapshot) DecisionFingerprint() string {
	canonical := canonicalHostSnapshot(s)
	if canonical.Identity.OSName == "" && canonical.Identity.OSVersion == "" && canonical.Identity.Architecture == "" && len(canonical.EncoderCapabilities) == 0 {
		return ""
	}
	keys := make([]decisionFingerprintEncoderKey, 0, len(canonical.EncoderCapabilities))
	seen := make(map[string]struct{}, len(canonical.EncoderCapabilities))
	for _, capability := range canonical.EncoderCapabilities {
		codec := normalizeToken(capability.Codec)
		if codec == "" {
			continue
		}
		if _, ok := seen[codec]; ok {
			continue
		}
		seen[codec] = struct{}{}
		keys = append(keys, decisionFingerprintEncoderKey{Codec: codec})
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Codec < keys[j].Codec
	})
	profileKeys := make([]decisionFingerprintProfileKey, 0, len(canonical.Runtime.Benchmark.Profiles))
	for _, benchmark := range canonical.Runtime.Benchmark.Profiles {
		if benchmark.ProfileID == "" {
			continue
		}
		profileKeys = append(profileKeys, decisionFingerprintProfileKey{
			ProfileID: benchmark.ProfileID,
			Class:     benchmark.Class,
		})
	}
	sort.Slice(profileKeys, func(i, j int) bool {
		return profileKeys[i].ProfileID < profileKeys[j].ProfileID
	})
	return "df4:" + sha256JSON(decisionFingerprintInput{
		Version:        "df4",
		HostClass:      canonical.Runtime.PerformanceClass,
		BenchmarkClass: playbackprofile.BenchmarkClassForCodec(canonical.Runtime.Benchmark, "h264"),
		ProfileKeys:    profileKeys,
		OSName:         canonical.Identity.OSName,
		OSVersion:      canonical.Identity.OSVersion,
		Architecture:   canonical.Identity.Architecture,
		EncoderKeys:    keys,
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

type FeedbackSummaryLookup interface {
	LookupRecentFeedbackSummary(ctx context.Context, query FeedbackSummaryQuery) (FeedbackSummary, bool, error)
}

type FeedbackObservationLookup interface {
	LookupRecentFeedbackObservations(ctx context.Context, query FeedbackSummaryQuery) ([]PlaybackObservation, error)
}

type PlaybackPolicyStateLookup interface {
	LookupPlaybackPolicyState(ctx context.Context, query PlaybackPolicyStateQuery) (PlaybackPolicyState, bool, error)
}

type PlaybackPolicyStateStore interface {
	RememberPlaybackPolicyState(ctx context.Context, state PlaybackPolicyState) error
}

type FeedbackSummaryQuery struct {
	SubjectKind       string
	SourceFingerprint string
	DeviceFingerprint string
	HostFingerprint   string
	Since             time.Time
	Limit             int
}

type PlaybackPolicyStateQuery struct {
	SubjectKind       string
	SourceFingerprint string
	DeviceFingerprint string
	HostFingerprint   string
}

type PlaybackPolicyState struct {
	SubjectKind       string
	SourceFingerprint string
	DeviceFingerprint string
	HostFingerprint   string
	MaxQualityRung    playbackprofile.QualityRung
	Confidence        runtimepolicy.ConfidenceSnapshot
	UpdatedAt         time.Time
}

type FeedbackSummary struct {
	LastObservedAt             time.Time
	SampleCount                int
	StartedCount               int
	WarningCount               int
	FailedCount                int
	ConsecutiveWarnings        int
	ConsecutiveBufferWarnings  int
	ConsecutiveDecodeWarnings  int
	ConsecutiveNetworkWarnings int
	ConsecutiveFailures        int
	ConsecutiveDecodeFailures  int
	ConsecutiveStallFailures   int
	PriorStartedStreak         int
	PriorRecoveredStartStreak  int
	PriorRecoveryStartCode     int
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
	policyState  map[string]PlaybackPolicyState
	observations []PlaybackObservation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		hosts:        make(map[string]HostSnapshot),
		devices:      make(map[string]DeviceSnapshot),
		sources:      make(map[string]SourceSnapshot),
		policyState:  make(map[string]PlaybackPolicyState),
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

func (s *MemoryStore) LookupRecentFeedbackSummary(_ context.Context, query FeedbackSummaryQuery) (FeedbackSummary, bool, error) {
	query = canonicalFeedbackSummaryQuery(query)
	if query.SourceFingerprint == "" || query.DeviceFingerprint == "" || query.HostFingerprint == "" {
		return FeedbackSummary{}, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	observations := make([]PlaybackObservation, 0, query.Limit)
	for idx := len(s.observations) - 1; idx >= 0 && len(observations) < query.Limit; idx-- {
		observation := s.observations[idx]
		if !matchesFeedbackSummaryQuery(observation, query) {
			continue
		}
		observations = append(observations, observation)
	}
	if len(observations) == 0 {
		return FeedbackSummary{}, false, nil
	}
	return summarizeFeedbackObservations(observations), true, nil
}

func (s *MemoryStore) LookupRecentFeedbackObservations(_ context.Context, query FeedbackSummaryQuery) ([]PlaybackObservation, error) {
	query = canonicalFeedbackSummaryQuery(query)
	if query.SourceFingerprint == "" || query.DeviceFingerprint == "" || query.HostFingerprint == "" {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	observations := make([]PlaybackObservation, 0, query.Limit)
	for idx := len(s.observations) - 1; idx >= 0 && len(observations) < query.Limit; idx-- {
		observation := s.observations[idx]
		if !matchesFeedbackSummaryQuery(observation, query) {
			continue
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func (s *MemoryStore) RememberPlaybackPolicyState(_ context.Context, state PlaybackPolicyState) error {
	state = canonicalPlaybackPolicyState(state)
	key := state.Fingerprint()
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policyState[key] = state
	return nil
}

func (s *MemoryStore) LookupPlaybackPolicyState(_ context.Context, query PlaybackPolicyStateQuery) (PlaybackPolicyState, bool, error) {
	query = canonicalPlaybackPolicyStateQuery(query)
	key := queryFingerprint(query)
	if key == "" {
		return PlaybackPolicyState{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.policyState[key]
	if !ok {
		return PlaybackPolicyState{}, false, nil
	}
	return state, true, nil
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
	out.BitrateConfidence = normalizeToken(out.BitrateConfidence)
	out.BitrateBucket = normalizeToken(out.BitrateBucket)
	if out.Width < 0 {
		out.Width = 0
	}
	if out.Height < 0 {
		out.Height = 0
	}
	if out.FPS < 0 {
		out.FPS = 0
	}
	if out.SignalFPS < 0 {
		out.SignalFPS = 0
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

func canonicalFeedbackSummaryQuery(in FeedbackSummaryQuery) FeedbackSummaryQuery {
	out := in
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.SourceFingerprint = strings.TrimSpace(out.SourceFingerprint)
	out.DeviceFingerprint = strings.TrimSpace(out.DeviceFingerprint)
	out.HostFingerprint = strings.TrimSpace(out.HostFingerprint)
	if out.Since.IsZero() {
		out.Since = time.Time{}
	} else {
		out.Since = out.Since.UTC()
	}
	if out.Limit <= 0 {
		out.Limit = 8
	}
	if out.Limit > 32 {
		out.Limit = 32
	}
	return out
}

func canonicalPlaybackPolicyStateQuery(in PlaybackPolicyStateQuery) PlaybackPolicyStateQuery {
	out := in
	out.SubjectKind = normalizeToken(out.SubjectKind)
	out.SourceFingerprint = strings.TrimSpace(out.SourceFingerprint)
	out.DeviceFingerprint = strings.TrimSpace(out.DeviceFingerprint)
	out.HostFingerprint = strings.TrimSpace(out.HostFingerprint)
	return out
}

func (s PlaybackPolicyState) Fingerprint() string {
	return queryFingerprint(canonicalPlaybackPolicyStateQuery(PlaybackPolicyStateQuery{
		SubjectKind:       s.SubjectKind,
		SourceFingerprint: s.SourceFingerprint,
		DeviceFingerprint: s.DeviceFingerprint,
		HostFingerprint:   s.HostFingerprint,
	}))
}

func canonicalPlaybackPolicyState(in PlaybackPolicyState) PlaybackPolicyState {
	out := in
	query := canonicalPlaybackPolicyStateQuery(PlaybackPolicyStateQuery{
		SubjectKind:       out.SubjectKind,
		SourceFingerprint: out.SourceFingerprint,
		DeviceFingerprint: out.DeviceFingerprint,
		HostFingerprint:   out.HostFingerprint,
	})
	out.SubjectKind = query.SubjectKind
	out.SourceFingerprint = query.SourceFingerprint
	out.DeviceFingerprint = query.DeviceFingerprint
	out.HostFingerprint = query.HostFingerprint
	out.MaxQualityRung = playbackprofile.NormalizeQualityRung(string(out.MaxQualityRung))
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	} else {
		out.UpdatedAt = out.UpdatedAt.UTC()
	}
	return out
}

func queryFingerprint(query PlaybackPolicyStateQuery) string {
	if query.SubjectKind == "" || query.SourceFingerprint == "" || query.DeviceFingerprint == "" || query.HostFingerprint == "" {
		return ""
	}
	return sha256JSON(map[string]any{
		"subjectKind":       query.SubjectKind,
		"sourceFingerprint": query.SourceFingerprint,
		"deviceFingerprint": query.DeviceFingerprint,
		"hostFingerprint":   query.HostFingerprint,
	})
}

func matchesFeedbackSummaryQuery(observation PlaybackObservation, query FeedbackSummaryQuery) bool {
	if observation.ObservationKind != "feedback" {
		return false
	}
	if query.SubjectKind != "" && observation.SubjectKind != query.SubjectKind {
		return false
	}
	if observation.SourceFingerprint != query.SourceFingerprint || observation.DeviceFingerprint != query.DeviceFingerprint || observation.HostFingerprint != query.HostFingerprint {
		return false
	}
	if !query.Since.IsZero() && observation.ObservedAt.Before(query.Since) {
		return false
	}
	return true
}

func summarizeFeedbackObservations(observations []PlaybackObservation) FeedbackSummary {
	summary := FeedbackSummary{}
	for idx, observation := range observations {
		if idx == 0 {
			summary.LastObservedAt = observation.ObservedAt
		}
		summary.SampleCount++
		switch observation.Outcome {
		case "started":
			summary.StartedCount++
		case "warning":
			summary.WarningCount++
		case "failed":
			summary.FailedCount++
		}
		if idx == 0 || summary.ConsecutiveFailures == idx {
			switch observation.Outcome {
			case "failed":
				summary.ConsecutiveFailures++
				switch observation.FeedbackCode {
				case 3:
					summary.ConsecutiveDecodeFailures++
				case 4:
					summary.ConsecutiveStallFailures++
				}
			default:
				// Only the newest uninterrupted failure streak should influence clamping.
			}
		}
		if idx == 0 || summary.ConsecutiveWarnings == idx {
			switch observation.Outcome {
			case "warning":
				summary.ConsecutiveWarnings++
				if isBufferingWarningCode(observation.FeedbackCode) {
					summary.ConsecutiveBufferWarnings++
				}
				if isDecodeWarningCode(observation.FeedbackCode) {
					summary.ConsecutiveDecodeWarnings++
				}
				if isNetworkWarningCode(observation.FeedbackCode) {
					summary.ConsecutiveNetworkWarnings++
				}
			default:
				// Only the newest uninterrupted warning streak should influence soft clamping.
			}
		}
	}
	if summary.ConsecutiveFailures > 0 {
		for _, observation := range observations[summary.ConsecutiveFailures:] {
			if observation.Outcome != "started" {
				break
			}
			summary.PriorStartedStreak++
		}
	}
	if summary.ConsecutiveWarnings > 0 {
		for _, observation := range observations[summary.ConsecutiveWarnings:] {
			if observation.Outcome != "started" || !isRecoveryStartCode(observation.FeedbackCode) {
				break
			}
			if summary.PriorRecoveredStartStreak == 0 {
				summary.PriorRecoveryStartCode = observation.FeedbackCode
			}
			summary.PriorRecoveredStartStreak++
		}
	}
	return summary
}

func isBufferingWarningCode(code int) bool {
	switch code {
	case 101, 102:
		return true
	default:
		return false
	}
}

func isDecodeWarningCode(code int) bool {
	switch code {
	case 103, 242:
		return true
	default:
		return false
	}
}

func isNetworkWarningCode(code int) bool {
	switch code {
	case 104:
		return true
	default:
		return false
	}
}

func isRecoveryStartCode(code int) bool {
	switch code {
	case 201, 211, 212, 213, 221, 231, 232, 241:
		return true
	default:
		return false
	}
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
