package runtimepolicy

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

type ConfidenceState string

const (
	ConfidenceLow      ConfidenceState = "LOW_CONFIDENCE"
	ConfidenceRecovery ConfidenceState = "RECOVERY"
	ConfidenceStable   ConfidenceState = "STABLE"
	ConfidenceHigh     ConfidenceState = "HIGH_CONFIDENCE"
)

type PolicyAction string

const (
	PolicyHold         PolicyAction = "hold"
	PolicyDegrade      PolicyAction = "degrade"
	PolicyStepDown     PolicyAction = "step_down"
	PolicyProbeUp      PolicyAction = "probe_up"
	PolicyConfirmProbe PolicyAction = "confirm_probe"
	PolicyAbortProbe   PolicyAction = "abort_probe"
	PolicyLockCurrent  PolicyAction = "lock_current"
	PolicyCooldown     PolicyAction = "cooldown"
)

const (
	ConstraintNoProbeUp            = "no_probe_up"
	ConstraintMaxQualityCompatible = "max_quality_compatible"
	ConstraintMaxQualityRepair     = "max_quality_repair"
	ConstraintLockCurrentRung      = "lock_current_rung"
	ConstraintCooldownActive       = "cooldown_active"
	ConstraintDecodeRiskHard       = "decode_risk_hard"
	ConstraintStartupWarmup        = "startup_warmup"
)

const (
	ReasonDecodeRiskHigh           = "decode_risk_high"
	ReasonDecodeWarningRecent      = "decode_warning_recent"
	ReasonStallRecent              = "stall_recent"
	ReasonNetworkRecentlyUnstable  = "network_recently_unstable"
	ReasonBufferingRecent          = "buffering_recent"
	ReasonBufferingRecovered       = "buffering_recently_recovered"
	ReasonNetworkRecovered         = "network_recently_recovered"
	ReasonDecoderRecovered         = "decoder_recently_recovered"
	ReasonCleanPlaybackWindow      = "clean_playback_window"
	ReasonHeadroomGood             = "headroom_good"
	ReasonHostPressureHigh         = "host_pressure_high"
	ReasonSourceTruthStale         = "source_truth_stale"
	ReasonSourceTruthLowConfidence = "source_truth_low_confidence"
	ReasonProbeRecentlyConfirmed   = "probe_recently_confirmed"
	ReasonProbeRecentlyRegressed   = "probe_recently_regressed"
	ReasonProbeWindowConfirmed     = "probe_window_confirmed"
	ReasonProbeWindowRegressed     = "probe_window_regressed"
	ReasonProbeUpReady             = "probe_up_ready"
	ReasonStartupWarmup            = "startup_warmup"
)

type WindowFeatures struct {
	HardDecodeFails         int
	HardStallFails          int
	BufferWarnings          int
	NetworkWarnings         int
	DecodeWarnings          int
	RecoveryBuffer          int
	RecoveryNetwork         int
	RecoveryDecode          int
	ProbeWindowStarted      int
	ProbeWindowConfirmed    int
	ProbeWindowRegressed    int
	CleanPlayingMS          int64
	WindowKind              string
	HostPressureBand        playbackprofile.HostPressureBand
	HostPerformanceClass    string
	HostBenchmarkClass      string
	SourceBitrateConfidence string
	SourceTruthFreshness    string
}

type ConfidenceSnapshot struct {
	Score              int
	State              ConfidenceState
	StateSince         time.Time
	WindowCount        int
	CooldownUntil      time.Time
	ProbeSuccessStreak int
	ProbeFailureStreak int
	LastProbeEventAt   time.Time
	PolicyConstraints  []string
	Reasons            []string
}

type PolicyInput struct {
	CurrentMaxQualityRung playbackprofile.QualityRung
}

type PolicyDecision struct {
	Action            PolicyAction
	MaxQualityRung    playbackprofile.QualityRung
	ProbeCandidate    playbackprofile.QualityRung
	PolicyConstraints []string
	Reasons           []string
}

type PlaybackLadderStep string

const (
	PlaybackStepUnknown           PlaybackLadderStep = ""
	PlaybackStepRepairLow         PlaybackLadderStep = "repair_low"
	PlaybackStepH264720p          PlaybackLadderStep = "h264_720p"
	PlaybackStepH2641080p         PlaybackLadderStep = "h264_1080p"
	PlaybackStepVideoCopyAudioAAC PlaybackLadderStep = "video_copy_audio_aac"
	PlaybackStepDirectCopy        PlaybackLadderStep = "direct_copy"
)

type ProbeLifecycleState string

const (
	ProbeLifecycleNone      ProbeLifecycleState = ""
	ProbeLifecycleScheduled ProbeLifecycleState = "scheduled"
	ProbeLifecycleObserving ProbeLifecycleState = "observing"
	ProbeLifecycleConfirmed ProbeLifecycleState = "confirmed"
	ProbeLifecycleAborted   ProbeLifecycleState = "aborted"
)

type SessionLoopState struct {
	CurrentStep       PlaybackLadderStep  `json:"currentStep,omitempty"`
	TargetStep        PlaybackLadderStep  `json:"targetStep,omitempty"`
	ProbeStep         PlaybackLadderStep  `json:"probeStep,omitempty"`
	ProbeState        ProbeLifecycleState `json:"probeState,omitempty"`
	ProbeStartedAt    time.Time           `json:"probeStartedAt,omitempty"`
	ProbeObservedAt   time.Time           `json:"probeObservedAt,omitempty"`
	ConfidenceScore   int                 `json:"confidenceScore,omitempty"`
	ConfidenceState   ConfidenceState     `json:"confidenceState,omitempty"`
	CooldownUntil     time.Time           `json:"cooldownUntil,omitempty"`
	LastTickAt        time.Time           `json:"lastTickAt,omitempty"`
	LastAction        PolicyAction        `json:"lastAction,omitempty"`
	PolicyConstraints []string            `json:"policyConstraints,omitempty"`
	Reasons           []string            `json:"reasons,omitempty"`
}

type SessionLoopInput struct {
	ObservedStep       PlaybackLadderStep
	TargetStep         PlaybackLadderStep
	Confidence         ConfidenceSnapshot
	StartupWarmupUntil time.Time
}

type SessionLoopDecision struct {
	Action            PolicyAction
	CurrentStep       PlaybackLadderStep
	TargetStep        PlaybackLadderStep
	ProbeStep         PlaybackLadderStep
	ProbeState        ProbeLifecycleState
	Blockers          []string
	PolicyConstraints []string
	Reasons           []string
}

type SessionTransitionKind string

const (
	SessionTransitionNoOp             SessionTransitionKind = ""
	SessionTransitionScheduleStepDown SessionTransitionKind = "schedule_step_down"
	SessionTransitionScheduleProbeUp  SessionTransitionKind = "schedule_probe_up"
	SessionTransitionCommitProbe      SessionTransitionKind = "commit_probe"
	SessionTransitionRevertProbe      SessionTransitionKind = "revert_probe"
	SessionTransitionForceRecoverLow  SessionTransitionKind = "force_recover_low"
)

type SessionTransition struct {
	Kind     SessionTransitionKind
	Action   PolicyAction
	FromStep PlaybackLadderStep
	ToStep   PlaybackLadderStep
	Reasons  []string
}

func (t SessionTransition) IsZero() bool {
	return t.Kind == SessionTransitionNoOp
}

const (
	BlockerCooldownActive         = "cooldown_active"
	BlockerProbeScheduled         = "probe_scheduled"
	BlockerProbeObserving         = "probe_observing"
	BlockerAlreadyAtLowestStep    = "already_at_lowest_step"
	BlockerAlreadyAtTarget        = "already_at_target"
	BlockerInsufficientConfidence = "insufficient_confidence"
	BlockerNoProbeUp              = "no_probe_up"
	BlockerStartupWarmup          = "startup_warmup"
	BlockerSessionNotRestartable  = "session_not_restartable"
	BlockerProfileUnmapped        = "profile_unmapped"
	BlockerAlreadyAtProfile       = "already_at_profile"
)

type TickTrace struct {
	TickAt                time.Time             `json:"tickAt"`
	ObservedStep          PlaybackLadderStep    `json:"observedStep,omitempty"`
	ConfidenceScore       int                   `json:"confidenceScore,omitempty"`
	ConfidenceState       ConfidenceState       `json:"confidenceState,omitempty"`
	ConfidenceStateSince  time.Time             `json:"confidenceStateSince,omitempty"`
	ConfidenceWindowCount int                   `json:"confidenceWindowCount,omitempty"`
	PolicyAction          PolicyAction          `json:"policyAction,omitempty"`
	PolicyConstraints     []string              `json:"policyConstraints,omitempty"`
	PlannedTransition     SessionTransitionKind `json:"plannedTransition,omitempty"`
	ExecutedTransition    SessionTransitionKind `json:"executedTransition,omitempty"`
	ActiveStep            PlaybackLadderStep    `json:"activeStep,omitempty"`
	TargetStep            PlaybackLadderStep    `json:"targetStep,omitempty"`
	ProbeStep             PlaybackLadderStep    `json:"probeStep,omitempty"`
	ProbeState            ProbeLifecycleState   `json:"probeState,omitempty"`
	CooldownUntil         time.Time             `json:"cooldownUntil,omitempty"`
	RuntimePhase          string                `json:"runtimePhase,omitempty"`
	Blockers              []string              `json:"blockers,omitempty"`
	Reasons               []string              `json:"reasons,omitempty"`
}
