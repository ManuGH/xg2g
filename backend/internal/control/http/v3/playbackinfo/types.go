// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackinfo

// PlaybackCapabilities Client capabilities for playback decision
type PlaybackCapabilities struct {
	AllowTranscode       *bool                       `json:"allowTranscode,omitempty"`
	AudioCodecs          []string                    `json:"audioCodecs"`
	CapabilitiesVersion  int                         `json:"capabilitiesVersion"`
	ClientFamilyFallback *string                     `json:"clientFamilyFallback,omitempty"`
	Container            []string                    `json:"container"`
	DeviceContext        *PlaybackDeviceContext      `json:"deviceContext,omitempty"`
	DeviceType           *string                     `json:"deviceType,omitempty"`
	HlsEngines           *[]string                   `json:"hlsEngines,omitempty"`
	MaxVideo             *PlaybackMaxVideo           `json:"maxVideo,omitempty"`
	NetworkContext       *PlaybackNetworkContext     `json:"networkContext,omitempty"`
	PreferredHlsEngine   *string                     `json:"preferredHlsEngine,omitempty"`
	RuntimeProbeUsed     *bool                       `json:"runtimeProbeUsed,omitempty"`
	RuntimeProbeVersion  *int                        `json:"runtimeProbeVersion,omitempty"`
	SupportsHls          *bool                       `json:"supportsHls,omitempty"`
	SupportsRange        *bool                       `json:"supportsRange,omitempty"`
	VideoCodecSignals    *[]PlaybackVideoCodecSignal `json:"videoCodecSignals,omitempty"`
	VideoCodecs          []string                    `json:"videoCodecs"`
}

type PlaybackMaxVideo struct {
	Fps    *int `json:"fps,omitempty"`
	Height *int `json:"height,omitempty"`
	Width  *int `json:"width,omitempty"`
}

type PlaybackDeviceContext struct {
	Brand        *string `json:"brand,omitempty"`
	Device       *string `json:"device,omitempty"`
	Manufacturer *string `json:"manufacturer,omitempty"`
	Model        *string `json:"model,omitempty"`
	OsName       *string `json:"osName,omitempty"`
	OsVersion    *string `json:"osVersion,omitempty"`
	Platform     *string `json:"platform,omitempty"`
	Product      *string `json:"product,omitempty"`
	SdkInt       *int    `json:"sdkInt,omitempty"`
}

type PlaybackNetworkContext struct {
	DownlinkKbps      *int    `json:"downlinkKbps,omitempty"`
	EffectiveType     *string `json:"effectiveType,omitempty"`
	InternetValidated *bool   `json:"internetValidated,omitempty"`
	Kind              *string `json:"kind,omitempty"`
	LinkSpeedMbps     *int    `json:"linkSpeedMbps,omitempty"`
	Metered           *bool   `json:"metered,omitempty"`
	SignalDbm         *int    `json:"signalDbm,omitempty"`
}

type PlaybackVideoCodecSignal struct {
	Codec             string `json:"codec"`
	DecodeError       *bool  `json:"decodeError,omitempty"`
	DecodeErrorStreak *int   `json:"decodeErrorStreak,omitempty"`
	IsSupported       bool   `json:"isSupported"`
	PowerEfficient    *bool  `json:"powerEfficient,omitempty"`
	ProbeSource       string `json:"probeSource"`
	Smooth            *bool  `json:"smooth,omitempty"`
	SuccessStreak     *int   `json:"successStreak,omitempty"`
	Supported         bool   `json:"supported"`
}

type PlaybackInfoMode string

const (
	PlaybackInfoModeDirectMp4 PlaybackInfoMode = "direct_mp4"
	PlaybackInfoModeHls       PlaybackInfoMode = "hls"
	PlaybackInfoModeDenied    PlaybackInfoMode = "deny"
	PlaybackInfoModeDeny      PlaybackInfoMode = "deny"
)

type PlaybackInfoReason string

const (
	PlaybackInfoReasonContainerMismatch PlaybackInfoReason = "container_mismatch"
	PlaybackInfoReasonDirectplayMatch   PlaybackInfoReason = "directplay_match"
	PlaybackInfoReasonTranscodeAll      PlaybackInfoReason = "transcode_all"
	PlaybackInfoReasonTranscodeAudio    PlaybackInfoReason = "transcode_audio"
	PlaybackInfoReasonTranscodeVideo    PlaybackInfoReason = "transcode_video"
	PlaybackInfoReasonUnknown           PlaybackInfoReason = "unknown"
)

type PlaybackInfoDurationSource string

type ResumeSummary struct {
	DurationSeconds *int64 `json:"durationSeconds,omitempty"`
	Finished        *bool  `json:"finished,omitempty"`
	PosSeconds      *int64 `json:"posSeconds,omitempty"`
	PositionMs      *int64 `json:"positionMs,omitempty"`
	UpdatedAt       *int64 `json:"updatedAt,omitempty"`
}

type PlaybackDecisionMode string

const (
	PlaybackDecisionModeDeny         PlaybackDecisionMode = "deny"
	PlaybackDecisionModeDirectPlay   PlaybackDecisionMode = "direct_play"
	PlaybackDecisionModeDirectStream PlaybackDecisionMode = "direct_stream"
	PlaybackDecisionModeTranscode    PlaybackDecisionMode = "transcode"
)

type PlaybackDecisionSelectedOutputKind string

const (
	PlaybackDecisionSelectedOutputKindHls PlaybackDecisionSelectedOutputKind = "hls"
)

type PlaybackOutput struct {
	Format string `json:"format"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Url    string `json:"url"`
}

type PlaybackDecisionSelected struct {
	AudioCodec string `json:"audioCodec"`
	Container  string `json:"container"`
	VideoCodec string `json:"videoCodec"`
}

type PlaybackTrace struct {
	AudioQualityRung          *string                `json:"audioQualityRung,omitempty"`
	AutoCodecBenchmarkClass   *string                `json:"autoCodecBenchmarkClass,omitempty"`
	AutoCodecPerformanceClass *string                `json:"autoCodecPerformanceClass,omitempty"`
	AutoCodecPolicy           *string                `json:"autoCodecPolicy,omitempty"`
	AutoCodecRequestedCodecs  *string                `json:"autoCodecRequestedCodecs,omitempty"`
	AutoCodecSelectedCodec    *string                `json:"autoCodecSelectedCodec,omitempty"`
	ClientCapsSource          *string                `json:"clientCapsSource,omitempty"`
	ClientFamily              *string                `json:"clientFamily,omitempty"`
	ClientPath                *string                `json:"clientPath,omitempty"`
	DegradedFrom              *string                `json:"degradedFrom,omitempty"`
	EffectiveModeSource       *string                `json:"effectiveModeSource,omitempty"`
	EffectiveRuntimeMode      *string                `json:"effectiveRuntimeMode,omitempty"`
	FallbackCount             *int                   `json:"fallbackCount,omitempty"`
	FirstFrameAtMs            *int                   `json:"firstFrameAtMs,omitempty"`
	HostOverrideApplied       *bool                  `json:"hostOverrideApplied,omitempty"`
	HostPressureBand          *string                `json:"hostPressureBand,omitempty"`
	InputKind                 *string                `json:"inputKind,omitempty"`
	LastFallbackReason        *string                `json:"lastFallbackReason,omitempty"`
	Operator                  *PlaybackTraceOperator `json:"operator,omitempty"`
	PolicyModeHint            *string                `json:"policyModeHint,omitempty"`
	PreflightDetail           *string                `json:"preflightDetail,omitempty"`
	PreflightReason           *string                `json:"preflightReason,omitempty"`
	QualityRung               *string                `json:"qualityRung,omitempty"`
	RequestId                 string                 `json:"requestId"`
	RequestProfile            *string                `json:"requestProfile,omitempty"`
	RequestedIntent           *string                `json:"requestedIntent,omitempty"`
	ResolvedIntent            *string                `json:"resolvedIntent,omitempty"`
	SessionId                 *string                `json:"sessionId,omitempty"`
	Source                    *PlaybackSourceProfile `json:"source,omitempty"`
	StopClass                 *string                `json:"stopClass,omitempty"`
	StopReason                *string                `json:"stopReason,omitempty"`
	TargetProfile             *PlaybackTargetProfile `json:"targetProfile,omitempty"`
	TargetProfileHash         *string                `json:"targetProfileHash,omitempty"`
	VideoQualityRung          *string                `json:"videoQualityRung,omitempty"`
}

type PlaybackTraceOperator struct {
	ClientFallbackDisabled    *bool     `json:"clientFallbackDisabled,omitempty"`
	ForcedIntent              *string   `json:"forcedIntent"`
	MaxQualityRung            *string   `json:"maxQualityRung"`
	OverrideApplied           *bool     `json:"overrideApplied,omitempty"`
	RuleName                  *string   `json:"ruleName"`
	RuleScope                 *string   `json:"ruleScope"`
	RuntimePolicyAction       *string   `json:"runtimePolicyAction"`
	RuntimePolicyConstraints  *[]string `json:"runtimePolicyConstraints"`
	RuntimePolicyPhase        *string   `json:"runtimePolicyPhase"`
	RuntimePolicyReasons      *[]string `json:"runtimePolicyReasons"`
	RuntimeProbeCandidate     *string   `json:"runtimeProbeCandidate"`
	RuntimeProbeFailureStreak *int      `json:"runtimeProbeFailureStreak"`
	RuntimeProbeSuccessStreak *int      `json:"runtimeProbeSuccessStreak"`
}

type PostLivePlaybackInfoJSONRequestBody struct {
	Capabilities PlaybackCapabilities `json:"capabilities"`
	ServiceRef   string               `json:"serviceRef"`
}

type PlaybackDecision struct {
	Constraints        []string                            `json:"constraints"`
	Mode               PlaybackDecisionMode                `json:"mode"`
	Outputs            []PlaybackOutput                    `json:"outputs"`
	Reasons            []string                            `json:"reasons"`
	Selected           PlaybackDecisionSelected            `json:"selected"`
	SelectedOutputKind PlaybackDecisionSelectedOutputKind `json:"selectedOutputKind"`
	SelectedOutputUrl  string                              `json:"selectedOutputUrl"`
	Trace              PlaybackTrace                       `json:"trace"`
}

type PlaybackSourceProfile struct {
	AudioBitrateKbps *int     `json:"audioBitrateKbps,omitempty"`
	AudioChannels    *int     `json:"audioChannels,omitempty"`
	AudioCodec       *string  `json:"audioCodec,omitempty"`
	BitrateKbps      *int     `json:"bitrateKbps,omitempty"`
	Container        *string  `json:"container,omitempty"`
	Fps              *float32 `json:"fps,omitempty"`
	Height           *int     `json:"height,omitempty"`
	Interlaced       *bool    `json:"interlaced,omitempty"`
	VideoCodec       *string  `json:"videoCodec,omitempty"`
	Width            *int     `json:"width,omitempty"`
}

type PlaybackTargetAudio struct {
	BitrateKbps int    `json:"bitrateKbps"`
	Channels    int    `json:"channels"`
	Codec       string `json:"codec"`
	Mode        string `json:"mode"`
	SampleRate  int    `json:"sampleRate"`
}

type PlaybackTargetHls struct {
	Enabled          bool   `json:"enabled"`
	SegmentContainer string `json:"segmentContainer"`
	SegmentSeconds   int    `json:"segmentSeconds"`
}

type PlaybackTargetVideo struct {
	Codec  string  `json:"codec"`
	Crf    *int    `json:"crf,omitempty"`
	Fps    float32 `json:"fps"`
	Height int     `json:"height"`
	Mode   string  `json:"mode"`
	Preset *string `json:"preset,omitempty"`
	Width  int     `json:"width"`
}

type PlaybackTargetProfile struct {
	Audio     PlaybackTargetAudio `json:"audio"`
	Container string              `json:"container"`
	Hls       PlaybackTargetHls   `json:"hls"`
	HwAccel   string              `json:"hwAccel"`
	Packaging string              `json:"packaging"`
	Video     PlaybackTargetVideo `json:"video"`
}

// PlaybackInfo represents the JSON response for playback-info operations.
type PlaybackInfo struct {
	AudioCodec            *string                     `json:"audioCodec,omitempty"`
	Container             *string                     `json:"container,omitempty"`
	Decision              *PlaybackDecision           `json:"decision,omitempty"`
	DecisionReason        *string                     `json:"decisionReason,omitempty"`
	DurationSeconds       *int64                      `json:"durationSeconds,omitempty"`
	DurationSource        *PlaybackInfoDurationSource `json:"durationSource,omitempty"`
	DvrWindowSeconds      *int64                      `json:"dvrWindowSeconds,omitempty"`
	IsSeekable            bool                        `json:"isSeekable"`
	LiveEdgeUnix          *int64                      `json:"liveEdgeUnix,omitempty"`
	Mode                  PlaybackInfoMode            `json:"mode"`
	PlaybackDecisionToken *string                     `json:"playbackDecisionToken,omitempty"`
	Reason                *PlaybackInfoReason         `json:"reason,omitempty"`
	RequestId             string                      `json:"requestId"`
	Resume                *ResumeSummary              `json:"resume,omitempty"`
	Seekable              *bool                       `json:"seekable,omitempty"`
	SessionId             string                      `json:"sessionId"`
	StartUnix             *int64                      `json:"startUnix,omitempty"`
	Url                   *string                     `json:"url,omitempty"`
	VideoCodec            *string                     `json:"videoCodec,omitempty"`
}
