package capreg

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

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
	for _, observation := range slices.Backward(s.observations) {
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
