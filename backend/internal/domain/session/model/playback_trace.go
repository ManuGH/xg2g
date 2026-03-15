// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package model

import "github.com/ManuGH/xg2g/internal/domain/playbackprofile"

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

type PlaybackTrace struct {
	Source            *playbackprofile.SourceProfile         `json:"source,omitempty"`
	RequestProfile    string                                 `json:"requestProfile,omitempty"`
	RequestedIntent   string                                 `json:"requestedIntent,omitempty"`
	ResolvedIntent    string                                 `json:"resolvedIntent,omitempty"`
	QualityRung       string                                 `json:"qualityRung,omitempty"`
	DegradedFrom      string                                 `json:"degradedFrom,omitempty"`
	ClientPath        string                                 `json:"clientPath,omitempty"`
	InputKind         string                                 `json:"inputKind,omitempty"`
	TargetProfileHash string                                 `json:"targetProfileHash,omitempty"`
	TargetProfile     *playbackprofile.TargetPlaybackProfile `json:"targetProfile,omitempty"`
	FFmpegPlan        *FFmpegPlanTrace                       `json:"ffmpegPlan,omitempty"`
	FirstFrameAtUnix  int64                                  `json:"firstFrameAtUnix,omitempty"`
	Fallbacks         []PlaybackFallbackTrace                `json:"fallbacks,omitempty"`
	StopReason        string                                 `json:"stopReason,omitempty"`
	StopClass         PlaybackStopClass                      `json:"stopClass,omitempty"`
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
	if len(t.Fallbacks) > 0 {
		cp.Fallbacks = append([]PlaybackFallbackTrace(nil), t.Fallbacks...)
	}
	return &cp
}
