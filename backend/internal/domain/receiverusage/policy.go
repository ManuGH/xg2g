// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidPolicyConfiguration = errors.New("invalid receiver usage policy configuration")
)

type ChannelChangePolicy struct {
	MinimumInterval time.Duration `json:"minimum_interval" yaml:"minimum_interval"`
	DuplicateWindow time.Duration `json:"duplicate_window" yaml:"duplicate_window"`
}

type ReceiverUsagePolicy struct {
	Mode                        ReceiverUsageMode     `json:"mode" yaml:"mode"`
	MaxLiveSessions             int                   `json:"max_live_sessions" yaml:"max_live_sessions"`
	MaxRecordingSessions        int                   `json:"max_recording_sessions" yaml:"max_recording_sessions"`
	MaxRestrictedAccessSessions int                   `json:"max_restricted_access_sessions" yaml:"max_restricted_access_sessions"`
	AllowLiveWithRecording      bool                  `json:"allow_live_with_recording" yaml:"allow_live_with_recording"`
	AllowTimeshift              bool                  `json:"allow_timeshift" yaml:"allow_timeshift"`
	AllowRetroDVRRestricted     bool                  `json:"allow_retro_dvr_restricted" yaml:"allow_retro_dvr_restricted"`
	UnknownAccessHandling       UnknownAccessHandling `json:"unknown_access_handling" yaml:"unknown_access_handling"`
	ChannelChangeLimiter        ChannelChangePolicy   `json:"channel_change_limiter" yaml:"channel_change_limiter"`
}

// Validate checks policy configuration. Zero values (e.g. MaxRecordingSessions = 0) are valid. Negative values or unknown enums are invalid.
func (p ReceiverUsagePolicy) Validate() error {
	switch p.Mode {
	case "", ReceiverUsageModeDisabled, ReceiverUsageModeAuditOnly, ReceiverUsageModeEnforce:
		// Valid
	default:
		return fmt.Errorf("%w: unknown mode %q", ErrInvalidPolicyConfiguration, p.Mode)
	}

	if p.MaxLiveSessions < 0 {
		return fmt.Errorf("%w: max_live_sessions cannot be negative (%d)", ErrInvalidPolicyConfiguration, p.MaxLiveSessions)
	}
	if p.MaxRecordingSessions < 0 {
		return fmt.Errorf("%w: max_recording_sessions cannot be negative (%d)", ErrInvalidPolicyConfiguration, p.MaxRecordingSessions)
	}
	if p.MaxRestrictedAccessSessions < 0 {
		return fmt.Errorf("%w: max_restricted_access_sessions cannot be negative (%d)", ErrInvalidPolicyConfiguration, p.MaxRestrictedAccessSessions)
	}

	switch p.UnknownAccessHandling {
	case "", UnknownAccessCountAsRestricted, UnknownAccessCountAsNone, UnknownAccessReject:
		// Valid
	default:
		return fmt.Errorf("%w: unknown unknown_access_handling enum %q", ErrInvalidPolicyConfiguration, p.UnknownAccessHandling)
	}

	if p.ChannelChangeLimiter.MinimumInterval < 0 {
		return fmt.Errorf("%w: minimum_interval cannot be negative (%v)", ErrInvalidPolicyConfiguration, p.ChannelChangeLimiter.MinimumInterval)
	}
	if p.ChannelChangeLimiter.DuplicateWindow < 0 {
		return fmt.Errorf("%w: duplicate_window cannot be negative (%v)", ErrInvalidPolicyConfiguration, p.ChannelChangeLimiter.DuplicateWindow)
	}

	return nil
}

// ConservativeSingleUsePolicy returns a strict default policy allowing 1 live TV session and 1 restricted access slot.
func ConservativeSingleUsePolicy() ReceiverUsagePolicy {
	return ReceiverUsagePolicy{
		Mode:                        ReceiverUsageModeEnforce,
		MaxLiveSessions:             1,
		MaxRecordingSessions:        1,
		MaxRestrictedAccessSessions: 1,
		AllowLiveWithRecording:      false,
		AllowTimeshift:              true,
		AllowRetroDVRRestricted:     true,
		UnknownAccessHandling:       UnknownAccessCountAsRestricted,
		ChannelChangeLimiter: ChannelChangePolicy{
			MinimumInterval: 5 * time.Second,
			DuplicateWindow: 5 * time.Second,
		},
	}
}
