// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package model

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

type PlaybackStopClass string

const (
	PlaybackStopClassNone     PlaybackStopClass = ""
	PlaybackStopClassInput    PlaybackStopClass = "input"
	PlaybackStopClassClient   PlaybackStopClass = "client"
	PlaybackStopClassPackager PlaybackStopClass = "packager"
	PlaybackStopClassServer   PlaybackStopClass = "server"
	PlaybackStopClassOperator PlaybackStopClass = "operator"
)

type PlaybackFallbackTrace struct {
	AtUnix          int64  `json:"atUnix,omitempty"`
	Trigger         string `json:"trigger,omitempty"`
	Reason          string `json:"reason,omitempty"`
	FromProfileHash string `json:"fromProfileHash,omitempty"`
	ToProfileHash   string `json:"toProfileHash,omitempty"`
}

type FFmpegPlanTrace struct {
	InputKind  string `json:"inputKind,omitempty"`
	Container  string `json:"container,omitempty"`
	Packaging  string `json:"packaging,omitempty"`
	HWAccel    string `json:"hwAccel,omitempty"`
	VideoMode  string `json:"videoMode,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioMode  string `json:"audioMode,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
}

type PlaybackOperatorTrace struct {
	ForcedIntent           string `json:"forcedIntent,omitempty"`
	MaxQualityRung         string `json:"maxQualityRung,omitempty"`
	ClientFallbackDisabled bool   `json:"clientFallbackDisabled,omitempty"`
	RuleName               string `json:"ruleName,omitempty"`
	RuleScope              string `json:"ruleScope,omitempty"`
	OverrideApplied        bool   `json:"overrideApplied,omitempty"`
}

type PlaybackClientDeviceContext struct {
	Brand        string `json:"brand,omitempty"`
	Device       string `json:"device,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	OSName       string `json:"osName,omitempty"`
	OSVersion    string `json:"osVersion,omitempty"`
	Platform     string `json:"platform,omitempty"`
	Product      string `json:"product,omitempty"`
	SDKInt       int    `json:"sdkInt,omitempty"`
}

type PlaybackClientNetworkContext struct {
	DownlinkKbps      int    `json:"downlinkKbps,omitempty"`
	InternetValidated *bool  `json:"internetValidated,omitempty"`
	Kind              string `json:"kind,omitempty"`
	Metered           *bool  `json:"metered,omitempty"`
}

type PlaybackClientSnapshot struct {
	CapturedAtUnix      int64                         `json:"capturedAtUnix,omitempty"`
	CapHash             string                        `json:"capHash,omitempty"`
	ClientCapsSource    string                        `json:"clientCapsSource,omitempty"`
	ClientFamily        string                        `json:"clientFamily,omitempty"`
	PreferredHLSEngine  string                        `json:"preferredHlsEngine,omitempty"`
	DeviceType          string                        `json:"deviceType,omitempty"`
	RuntimeProbeUsed    bool                          `json:"runtimeProbeUsed,omitempty"`
	RuntimeProbeVersion int                           `json:"runtimeProbeVersion,omitempty"`
	DeviceContext       *PlaybackClientDeviceContext  `json:"deviceContext,omitempty"`
	NetworkContext      *PlaybackClientNetworkContext `json:"networkContext,omitempty"`
}

func (s *PlaybackClientSnapshot) Clone() *PlaybackClientSnapshot {
	if s == nil {
		return nil
	}
	cp := *s
	if s.DeviceContext != nil {
		device := *s.DeviceContext
		cp.DeviceContext = &device
	}
	if s.NetworkContext != nil {
		network := *s.NetworkContext
		cp.NetworkContext = &network
	}
	return &cp
}

type HLSAccessTrace struct {
	PlaylistRequestCount   int      `json:"playlistRequestCount,omitempty"`
	LastPlaylistAtUnix     int64    `json:"lastPlaylistAtUnix,omitempty"`
	LastPlaylistIntervalMs int      `json:"lastPlaylistIntervalMs,omitempty"`
	SegmentRequestCount    int      `json:"segmentRequestCount,omitempty"`
	LastSegmentAtUnix      int64    `json:"lastSegmentAtUnix,omitempty"`
	LastSegmentName        string   `json:"lastSegmentName,omitempty"`
	LastSegmentGapMs       int      `json:"lastSegmentGapMs,omitempty"`
	LatestSegmentLagMs     int      `json:"latestSegmentLagMs,omitempty"`
	StallRisk              string   `json:"stallRisk,omitempty"`
	StartupMode            string   `json:"startupMode,omitempty"`
	StartupHeadroomSec     int      `json:"startupHeadroomSec,omitempty"`
	StartupReasons         []string `json:"startupReasons,omitempty"`
}

type PlaybackTrace struct {
	Source               *playbackprofile.SourceProfile         `json:"source,omitempty"`
	RequestProfile       string                                 `json:"requestProfile,omitempty"`
	RequestedIntent      string                                 `json:"requestedIntent,omitempty"`
	ResolvedIntent       string                                 `json:"resolvedIntent,omitempty"`
	PolicyModeHint       ports.RuntimeMode                      `json:"policyModeHint,omitempty"`
	EffectiveRuntimeMode ports.RuntimeMode                      `json:"effectiveRuntimeMode,omitempty"`
	EffectiveModeSource  ports.RuntimeModeSource                `json:"effectiveModeSource,omitempty"`
	QualityRung          string                                 `json:"qualityRung,omitempty"`
	AudioQualityRung     string                                 `json:"audioQualityRung,omitempty"`
	VideoQualityRung     string                                 `json:"videoQualityRung,omitempty"`
	DegradedFrom         string                                 `json:"degradedFrom,omitempty"`
	ClientPath           string                                 `json:"clientPath,omitempty"`
	InputKind            string                                 `json:"inputKind,omitempty"`
	PreflightReason      string                                 `json:"preflightReason,omitempty"`
	PreflightDetail      string                                 `json:"preflightDetail,omitempty"`
	TargetProfileHash    string                                 `json:"targetProfileHash,omitempty"`
	TargetProfile        *playbackprofile.TargetPlaybackProfile `json:"targetProfile,omitempty"`
	FFmpegPlan           *FFmpegPlanTrace                       `json:"ffmpegPlan,omitempty"`
	Operator             *PlaybackOperatorTrace                 `json:"operator,omitempty"`
	Client               *PlaybackClientSnapshot                `json:"client,omitempty"`
	HLS                  *HLSAccessTrace                        `json:"hls,omitempty"`
	HostPressureBand     string                                 `json:"hostPressureBand,omitempty"`
	HostOverrideApplied  bool                                   `json:"hostOverrideApplied,omitempty"`
	FirstFrameAtUnix     int64                                  `json:"firstFrameAtUnix,omitempty"`
	Fallbacks            []PlaybackFallbackTrace                `json:"fallbacks,omitempty"`
	StopReason           string                                 `json:"stopReason,omitempty"`
	StopClass            PlaybackStopClass                      `json:"stopClass,omitempty"`
}

func (t *PlaybackTrace) Clone() *PlaybackTrace {
	if t == nil {
		return nil
	}

	cp := *t
	if t.Source != nil {
		source := *t.Source
		cp.Source = &source
	}
	if t.TargetProfile != nil {
		target := *t.TargetProfile
		cp.TargetProfile = &target
	}
	if t.FFmpegPlan != nil {
		plan := *t.FFmpegPlan
		cp.FFmpegPlan = &plan
	}
	if t.Operator != nil {
		operator := *t.Operator
		cp.Operator = &operator
	}
	if t.Client != nil {
		client := *t.Client
		if t.Client.DeviceContext != nil {
			device := *t.Client.DeviceContext
			client.DeviceContext = &device
		}
		if t.Client.NetworkContext != nil {
			network := *t.Client.NetworkContext
			client.NetworkContext = &network
		}
		cp.Client = &client
	}
	if t.HLS != nil {
		hls := *t.HLS
		if len(t.HLS.StartupReasons) > 0 {
			hls.StartupReasons = append([]string(nil), t.HLS.StartupReasons...)
		}
		cp.HLS = &hls
	}
	if len(t.Fallbacks) > 0 {
		cp.Fallbacks = append([]PlaybackFallbackTrace(nil), t.Fallbacks...)
	}
	return &cp
}
