// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"

	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
)

func mapPlaybackInfoToOpenAPI(in v3playbackinfo.PlaybackInfo) PlaybackInfo {
	out := PlaybackInfo{
		AudioCodec:            in.AudioCodec,
		Container:             in.Container,
		DecisionReason:        in.DecisionReason,
		DurationSeconds:       in.DurationSeconds,
		DvrWindowSeconds:      in.DvrWindowSeconds,
		IsSeekable:            in.IsSeekable,
		LiveEdgeUnix:          in.LiveEdgeUnix,
		Mode:                  PlaybackInfoMode(in.Mode),
		PlaybackDecisionToken: in.PlaybackDecisionToken,
		RequestId:             in.RequestId,
		Seekable:              in.Seekable,
		SessionId:             in.SessionId,
		StartUnix:             in.StartUnix,
		Url:                   in.Url,
		VideoCodec:            in.VideoCodec,
	}

	if in.DurationSource != nil {
		ds := PlaybackInfoDurationSource(*in.DurationSource)
		out.DurationSource = &ds
	}
	if in.Reason != nil {
		r := PlaybackInfoReason(*in.Reason)
		out.Reason = &r
	}
	if in.Resume != nil {
		out.Resume = &ResumeSummary{
			DurationSeconds: in.Resume.DurationSeconds,
			Finished:        in.Resume.Finished,
			PosSeconds:      in.Resume.PosSeconds,
			UpdatedAt:       in.Resume.UpdatedAt,
		}
	}
	if in.Decision != nil {
		dec := &PlaybackDecision{
			Constraints:        in.Decision.Constraints,
			Mode:               PlaybackDecisionMode(in.Decision.Mode),
			Reasons:            in.Decision.Reasons,
			SelectedOutputKind: PlaybackDecisionSelectedOutputKind(in.Decision.SelectedOutputKind),
			SelectedOutputUrl:  in.Decision.SelectedOutputUrl,
			Selected: struct {
				AudioCodec string `json:"audioCodec"`
				Container  string `json:"container"`
				VideoCodec string `json:"videoCodec"`
			}{
				AudioCodec: in.Decision.Selected.AudioCodec,
				Container:  in.Decision.Selected.Container,
				VideoCodec: in.Decision.Selected.VideoCodec,
			},
		}

		for _, o := range in.Decision.Outputs {
			switch o.Kind {
			case "hls":
				raw, _ := json.Marshal(PlaybackOutputHls{
					Kind:        Hls,
					PlaylistUrl: o.Url,
				})
				if raw != nil {
					var po PlaybackOutput
					_ = po.UnmarshalJSON(raw)
					dec.Outputs = append(dec.Outputs, po)
				}
			case "file":
				raw, _ := json.Marshal(PlaybackOutputFile{
					Kind: PlaybackOutputFileKindFile,
					Url:  o.Url,
				})
				if raw != nil {
					var po PlaybackOutput
					_ = po.UnmarshalJSON(raw)
					dec.Outputs = append(dec.Outputs, po)
				}
			}
		}

		dec.Trace = mapPlaybackTraceToOpenAPI(in.Decision.Trace)
		out.Decision = dec
	}

	return out
}

func mapPlaybackTraceToOpenAPI(t v3playbackinfo.PlaybackTrace) PlaybackTrace {
	trace := PlaybackTrace{
		AudioQualityRung:          t.AudioQualityRung,
		AutoCodecBenchmarkClass:   t.AutoCodecBenchmarkClass,
		AutoCodecPerformanceClass: t.AutoCodecPerformanceClass,
		AutoCodecPolicy:           t.AutoCodecPolicy,
		AutoCodecRequestedCodecs:  t.AutoCodecRequestedCodecs,
		AutoCodecSelectedCodec:    t.AutoCodecSelectedCodec,
		ClientCapsSource:          t.ClientCapsSource,
		ClientFamily:              t.ClientFamily,
		ClientPath:                t.ClientPath,
		DegradedFrom:              t.DegradedFrom,
		EffectiveModeSource:       t.EffectiveModeSource,
		EffectiveRuntimeMode:      t.EffectiveRuntimeMode,
		FallbackCount:             t.FallbackCount,
		FirstFrameAtMs:            t.FirstFrameAtMs,
		HostOverrideApplied:       t.HostOverrideApplied,
		HostPressureBand:          t.HostPressureBand,
		InputKind:                 t.InputKind,
		LastFallbackReason:        t.LastFallbackReason,
		PolicyModeHint:            t.PolicyModeHint,
		PreflightDetail:           t.PreflightDetail,
		PreflightReason:           t.PreflightReason,
		QualityRung:               t.QualityRung,
		RequestId:                 t.RequestId,
		RequestProfile:            t.RequestProfile,
		RequestedIntent:           t.RequestedIntent,
		ResolvedIntent:            t.ResolvedIntent,
		SessionId:                 t.SessionId,
		StopClass:                 t.StopClass,
		StopReason:                t.StopReason,
		TargetProfileHash:         t.TargetProfileHash,
		VideoQualityRung:          t.VideoQualityRung,
	}

	if t.Source != nil {
		trace.Source = &PlaybackSourceProfile{
			AudioBitrateKbps: t.Source.AudioBitrateKbps,
			AudioChannels:    t.Source.AudioChannels,
			AudioCodec:       t.Source.AudioCodec,
			BitrateKbps:      t.Source.BitrateKbps,
			Container:        t.Source.Container,
			Fps:              t.Source.Fps,
			Height:           t.Source.Height,
			Interlaced:       t.Source.Interlaced,
			VideoCodec:       t.Source.VideoCodec,
			Width:            t.Source.Width,
		}
	}

	if t.TargetProfile != nil {
		trace.TargetProfile = &PlaybackTargetProfile{
			Audio: PlaybackTargetAudio{
				BitrateKbps: t.TargetProfile.Audio.BitrateKbps,
				Channels:    t.TargetProfile.Audio.Channels,
				Codec:       t.TargetProfile.Audio.Codec,
				Mode:        t.TargetProfile.Audio.Mode,
				SampleRate:  t.TargetProfile.Audio.SampleRate,
			},
			Container: t.TargetProfile.Container,
			Hls: PlaybackTargetHls{
				Enabled:          t.TargetProfile.Hls.Enabled,
				SegmentContainer: t.TargetProfile.Hls.SegmentContainer,
				SegmentSeconds:   t.TargetProfile.Hls.SegmentSeconds,
			},
			HwAccel:   t.TargetProfile.HwAccel,
			Packaging: t.TargetProfile.Packaging,
			Video: PlaybackTargetVideo{
				Codec:  t.TargetProfile.Video.Codec,
				Crf:    t.TargetProfile.Video.Crf,
				Fps:    t.TargetProfile.Video.Fps,
				Height: t.TargetProfile.Video.Height,
				Mode:   t.TargetProfile.Video.Mode,
				Preset: t.TargetProfile.Video.Preset,
				Width:  t.TargetProfile.Video.Width,
			},
		}
	}

	if t.Operator != nil {
		trace.Operator = &PlaybackTraceOperator{
			ClientFallbackDisabled:    t.Operator.ClientFallbackDisabled,
			ForcedIntent:              t.Operator.ForcedIntent,
			MaxQualityRung:            t.Operator.MaxQualityRung,
			OverrideApplied:           t.Operator.OverrideApplied,
			RuleName:                  t.Operator.RuleName,
			RuleScope:                 t.Operator.RuleScope,
			RuntimePolicyAction:       t.Operator.RuntimePolicyAction,
			RuntimePolicyConstraints:  t.Operator.RuntimePolicyConstraints,
			RuntimePolicyPhase:        t.Operator.RuntimePolicyPhase,
			RuntimePolicyReasons:      t.Operator.RuntimePolicyReasons,
			RuntimeProbeCandidate:     t.Operator.RuntimeProbeCandidate,
			RuntimeProbeFailureStreak: t.Operator.RuntimeProbeFailureStreak,
			RuntimeProbeSuccessStreak: t.Operator.RuntimeProbeSuccessStreak,
		}
	}

	return trace
}

func mapCapabilitiesToPlaybackInfoSubpackage(c *PlaybackCapabilities) *v3playbackinfo.PlaybackCapabilities {
	if c == nil {
		return nil
	}
	sub := &v3playbackinfo.PlaybackCapabilities{
		AllowTranscode:       c.AllowTranscode,
		AudioCodecs:          c.AudioCodecs,
		CapabilitiesVersion:  c.CapabilitiesVersion,
		ClientFamilyFallback: c.ClientFamilyFallback,
		Container:            c.Container,
		DeviceType:           c.DeviceType,
		HlsEngines:           c.HlsEngines,
		PreferredHlsEngine:   c.PreferredHlsEngine,
		RuntimeProbeUsed:     c.RuntimeProbeUsed,
		RuntimeProbeVersion:  c.RuntimeProbeVersion,
		SupportsHls:          c.SupportsHls,
		SupportsRange:        c.SupportsRange,
		VideoCodecs:          c.VideoCodecs,
	}
	if c.DeviceContext != nil {
		sub.DeviceContext = &v3playbackinfo.PlaybackDeviceContext{
			Brand:        c.DeviceContext.Brand,
			Device:       c.DeviceContext.Device,
			Manufacturer: c.DeviceContext.Manufacturer,
			Model:        c.DeviceContext.Model,
			OsName:       c.DeviceContext.OsName,
			OsVersion:    c.DeviceContext.OsVersion,
			Platform:     c.DeviceContext.Platform,
			Product:      c.DeviceContext.Product,
			SdkInt:       c.DeviceContext.SdkInt,
		}
	}
	if c.MaxVideo != nil {
		sub.MaxVideo = &v3playbackinfo.PlaybackMaxVideo{
			Fps:    c.MaxVideo.Fps,
			Height: c.MaxVideo.Height,
			Width:  c.MaxVideo.Width,
		}
	}
	if c.NetworkContext != nil {
		sub.NetworkContext = &v3playbackinfo.PlaybackNetworkContext{
			DownlinkKbps:      c.NetworkContext.DownlinkKbps,
			InternetValidated: c.NetworkContext.InternetValidated,
			Kind:              c.NetworkContext.Kind,
			Metered:           c.NetworkContext.Metered,
		}
	}
	if c.VideoCodecSignals != nil {
		signals := make([]v3playbackinfo.PlaybackVideoCodecSignal, len(*c.VideoCodecSignals))
		for i, sig := range *c.VideoCodecSignals {
			signals[i] = v3playbackinfo.PlaybackVideoCodecSignal{
				Codec:          sig.Codec,
				PowerEfficient: sig.PowerEfficient,
				Smooth:         sig.Smooth,
				Supported:      sig.Supported,
			}
		}
		sub.VideoCodecSignals = &signals
	}
	return sub
}

func mapPlaybackSourceProfileToV3(src *v3playbackinfo.PlaybackSourceProfile) *PlaybackSourceProfile {
	if src == nil {
		return nil
	}
	return &PlaybackSourceProfile{
		AudioBitrateKbps: src.AudioBitrateKbps,
		AudioChannels:    src.AudioChannels,
		AudioCodec:       src.AudioCodec,
		BitrateKbps:      src.BitrateKbps,
		Container:        src.Container,
		Fps:              src.Fps,
		Height:           src.Height,
		Interlaced:       src.Interlaced,
		VideoCodec:       src.VideoCodec,
		Width:            src.Width,
	}
}

func mapPlaybackTargetProfileToV3(target *v3playbackinfo.PlaybackTargetProfile) *PlaybackTargetProfile {
	if target == nil {
		return nil
	}
	return &PlaybackTargetProfile{
		Audio: PlaybackTargetAudio{
			BitrateKbps: target.Audio.BitrateKbps,
			Channels:    target.Audio.Channels,
			Codec:       target.Audio.Codec,
			Mode:        target.Audio.Mode,
			SampleRate:  target.Audio.SampleRate,
		},
		Container: target.Container,
		Hls: PlaybackTargetHls{
			Enabled:          target.Hls.Enabled,
			SegmentContainer: target.Hls.SegmentContainer,
			SegmentSeconds:   target.Hls.SegmentSeconds,
		},
		HwAccel:   target.HwAccel,
		Packaging: target.Packaging,
		Video: PlaybackTargetVideo{
			Codec:  target.Video.Codec,
			Crf:    target.Video.Crf,
			Fps:    target.Video.Fps,
			Height: target.Video.Height,
			Mode:   target.Video.Mode,
			Preset: target.Video.Preset,
			Width:  target.Video.Width,
		},
	}
}
