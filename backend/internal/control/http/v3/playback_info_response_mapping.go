// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/hls"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
)

func (s *Server) mapPlaybackInfoV2(ctx context.Context, id string, dec *decision.Decision, rState *resume.State, truth *hls.SegmentTruth, attemptedTruth bool, rawTruth playback.MediaTruth, schemaType string, caps *PlaybackCapabilities, resolvedCaps capabilities.PlaybackCapabilities, requestProfile, operatorRuleName, operatorRuleScope string) PlaybackInfo {
	proto := decision.ProtocolFrom(dec)
	var mode PlaybackInfoMode
	var url string

	switch proto {
	case "mp4":
		mode = PlaybackInfoModeDirectMp4
		if schemaType == "live" {
			url = fmt.Sprintf("/api/v3/streams/%s", id)
		} else {
			url = fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", id)
		}
	case "hls":
		mode = PlaybackInfoModeHls
		if schemaType == "live" {
			url = fmt.Sprintf("/api/v3/streams/%s/playlist.m3u8", id)
		} else {
			url = v3recordings.RecordingPlaylistURL(id, requestProfile, dec.TargetProfile)
		}
	case "none":
		mode = PlaybackInfoModeDirectMp4
		url = ""
	}

	primaryStr := decision.ReasonPrimaryFrom(dec, nil)
	mainReason := PlaybackInfoReason(primaryStr)
	decDTO := buildPlaybackDecisionDTO(id, dec, url, resolvedCaps, requestProfile, operatorRuleName, operatorRuleScope)
	resDTO := buildPlaybackResumeSummary(rState)

	var finalURL *string
	if url != "" {
		finalURL = &url
	}

	durSec := int64(math.Round(rawTruth.Duration))
	container := rawTruth.Container
	videoCodec := rawTruth.VideoCodec
	audioCodec := rawTruth.AudioCodec

	info := PlaybackInfo{
		Mode:                  mode,
		DecisionReason:        (*string)(&primaryStr),
		Url:                   finalURL,
		DurationSeconds:       &durSec,
		DurationSource:        nil,
		Container:             &container,
		VideoCodec:            &videoCodec,
		AudioCodec:            &audioCodec,
		Reason:                &mainReason,
		Decision:              &decDTO,
		Resume:                resDTO,
		RequestId:             dec.Trace.RequestID,
		SessionId:             fmt.Sprintf("rec:%s", id),
		PlaybackDecisionToken: s.buildLivePlaybackDecisionToken(id, dec, schemaType, caps),
	}

	applySegmentTruth(&info, truth, attemptedTruth)
	return info
}

func buildPlaybackDecisionDTO(id string, dec *decision.Decision, url string, resolvedCaps capabilities.PlaybackCapabilities, requestProfile, operatorRuleName, operatorRuleScope string) PlaybackDecision {
	var decDTO PlaybackDecision
	decDTO.Mode = PlaybackDecisionMode(dec.Mode)
	decDTO.Selected.Container = dec.Selected.Container
	decDTO.Selected.VideoCodec = dec.Selected.VideoCodec
	decDTO.Selected.AudioCodec = dec.Selected.AudioCodec
	decDTO.SelectedOutputUrl = dec.SelectedOutputURL
	if len(decDTO.SelectedOutputUrl) >= len("placeholder://") && decDTO.SelectedOutputUrl[:len("placeholder://")] == "placeholder://" {
		decDTO.SelectedOutputUrl = url
	}
	decDTO.SelectedOutputKind = PlaybackDecisionSelectedOutputKind(dec.SelectedOutputKind)

	for _, out := range dec.Outputs {
		var raw json.RawMessage
		effectiveURL := out.URL
		if len(effectiveURL) >= len("placeholder://") && effectiveURL[:len("placeholder://")] == "placeholder://" {
			effectiveURL = url
		}

		switch out.Kind {
		case "file":
			raw, _ = json.Marshal(PlaybackOutputFile{
				Kind: PlaybackOutputFileKindFile,
				Url:  effectiveURL,
			})
		case "hls":
			raw, _ = json.Marshal(PlaybackOutputHls{
				Kind:        Hls,
				PlaylistUrl: effectiveURL,
			})
		}
		if raw != nil {
			var po PlaybackOutput
			_ = po.UnmarshalJSON(raw)
			decDTO.Outputs = append(decDTO.Outputs, po)
		}
	}

	decDTO.Trace.RequestId = dec.Trace.RequestID
	sessionID := fmt.Sprintf("rec:%s", id)
	decDTO.Trace.SessionId = &sessionID
	if rp := normalize.Token(requestProfile); rp != "" {
		publicProfile := profiles.PublicProfileName(rp)
		if publicProfile == "" {
			publicProfile = rp
		}
		decDTO.Trace.RequestProfile = &publicProfile
	}
	if dec.Trace.RequestedIntent != "" {
		intent := dec.Trace.RequestedIntent
		decDTO.Trace.RequestedIntent = &intent
	}
	if dec.Trace.ResolvedIntent != "" {
		intent := dec.Trace.ResolvedIntent
		decDTO.Trace.ResolvedIntent = &intent
	}
	if dec.Trace.QualityRung != "" {
		rung := dec.Trace.QualityRung
		decDTO.Trace.QualityRung = &rung
	}
	if dec.Trace.AudioQualityRung != "" {
		rung := dec.Trace.AudioQualityRung
		decDTO.Trace.AudioQualityRung = &rung
	}
	if dec.Trace.VideoQualityRung != "" {
		rung := dec.Trace.VideoQualityRung
		decDTO.Trace.VideoQualityRung = &rung
	}
	if dec.Trace.DegradedFrom != "" {
		intent := dec.Trace.DegradedFrom
		decDTO.Trace.DegradedFrom = &intent
	}
	if dec.Trace.HostPressureBand != "" {
		hostPressureBand := dec.Trace.HostPressureBand
		decDTO.Trace.HostPressureBand = &hostPressureBand
	}
	if dec.Trace.HostOverrideApplied {
		hostOverrideApplied := true
		decDTO.Trace.HostOverrideApplied = &hostOverrideApplied
	}
	if resolvedCaps.ClientCapsSource != "" {
		clientCapsSource := resolvedCaps.ClientCapsSource
		decDTO.Trace.ClientCapsSource = &clientCapsSource
	}
	if resolvedCaps.ClientFamilyFallback != "" {
		clientFamily := resolvedCaps.ClientFamilyFallback
		decDTO.Trace.ClientFamily = &clientFamily
	}
	if dec.Trace.ForcedIntent != "" || dec.Trace.MaxQualityRung != "" || dec.Trace.OverrideApplied {
		operator := PlaybackTraceOperator{
			ClientFallbackDisabled: boolPtr(false),
			OverrideApplied:        boolPtr(dec.Trace.OverrideApplied),
		}
		if dec.Trace.ForcedIntent != "" {
			forcedIntent := dec.Trace.ForcedIntent
			operator.ForcedIntent = &forcedIntent
		}
		if dec.Trace.MaxQualityRung != "" {
			maxQualityRung := dec.Trace.MaxQualityRung
			operator.MaxQualityRung = &maxQualityRung
		}
		if operatorRuleName != "" {
			ruleName := operatorRuleName
			operator.RuleName = &ruleName
		}
		if operatorRuleScope != "" {
			ruleScope := operatorRuleScope
			operator.RuleScope = &ruleScope
		}
		decDTO.Trace.Operator = &operator
	}
	decDTO.Reasons = decision.ReasonsAsStrings(dec, nil)
	if dec.TargetProfile != nil {
		hash := dec.TargetProfile.Hash()
		decDTO.Trace.TargetProfileHash = &hash
		decDTO.Trace.TargetProfile = mapTargetProfile(dec.TargetProfile)
	}

	return decDTO
}

func buildPlaybackResumeSummary(rState *resume.State) *ResumeSummary {
	if rState == nil {
		return nil
	}

	finished := rState.Finished
	var duration *int64
	if rState.DurationSeconds > 0 {
		value := rState.DurationSeconds
		duration = &value
	}

	return &ResumeSummary{
		PosSeconds:      rState.PosSeconds,
		DurationSeconds: duration,
		Finished:        &finished,
	}
}

func (s *Server) buildLivePlaybackDecisionToken(id string, dec *decision.Decision, schemaType string, caps *PlaybackCapabilities) *string {
	s.mu.RLock()
	jwtSecret := append([]byte(nil), s.JWTSecret...)
	s.mu.RUnlock()

	if schemaType != "live" || dec.Mode == decision.ModeDeny || len(jwtSecret) == 0 {
		return nil
	}

	now := time.Now().Unix()
	var capHash string
	if caps != nil {
		if capBytes, err := json.Marshal(caps); err == nil {
			var capMap map[string]any
			if err := json.Unmarshal(capBytes, &capMap); err == nil {
				if hash, err := normalize.MapHash(capMap); err == nil {
					capHash = hash
				}
			}
		}
	}

	claims := v3auth.TokenClaims{
		Iss:     "xg2g",
		Aud:     "xg2g/v3/intents",
		Sub:     normalize.ServiceRef(id),
		Jti:     uuid.New().String(),
		Iat:     now,
		Nbf:     now,
		Exp:     now + 60,
		Mode:    string(dec.Mode),
		CapHash: capHash,
		TraceID: dec.Trace.RequestID,
	}

	tokenStr, err := v3auth.GenerateHS256(jwtSecret, claims, "kid-v1")
	if err != nil {
		log.L().Error().Err(err).Str("id", id).Msg("failed to generate secure playback token")
		return nil
	}

	return &tokenStr
}

func mapTargetProfile(target *playbackprofile.TargetPlaybackProfile) *PlaybackTargetProfile {
	if target == nil {
		return nil
	}
	canonical := playbackprofile.CanonicalizeTarget(*target)
	var crf *int
	if canonical.Video.CRF > 0 {
		value := canonical.Video.CRF
		crf = &value
	}
	var preset *string
	if canonical.Video.Preset != "" {
		value := canonical.Video.Preset
		preset = &value
	}
	return &PlaybackTargetProfile{
		Container: string(canonical.Container),
		Packaging: string(canonical.Packaging),
		HwAccel:   string(canonical.HWAccel),
		Video: PlaybackTargetVideo{
			Mode:   string(canonical.Video.Mode),
			Codec:  canonical.Video.Codec,
			Crf:    crf,
			Preset: preset,
			Width:  canonical.Video.Width,
			Height: canonical.Video.Height,
			Fps:    float32(canonical.Video.FPS),
		},
		Audio: PlaybackTargetAudio{
			Mode:        string(canonical.Audio.Mode),
			Codec:       canonical.Audio.Codec,
			Channels:    canonical.Audio.Channels,
			BitrateKbps: canonical.Audio.BitrateKbps,
			SampleRate:  canonical.Audio.SampleRate,
		},
		Hls: PlaybackTargetHls{
			Enabled:          canonical.HLS.Enabled,
			SegmentContainer: canonical.HLS.SegmentContainer,
			SegmentSeconds:   canonical.HLS.SegmentSeconds,
		},
	}
}

func mapSourceProfile(source *playbackprofile.SourceProfile) *PlaybackSourceProfile {
	if source == nil {
		return nil
	}
	canonical := playbackprofile.CanonicalizeSource(*source)
	var bitrateKbps *int
	if canonical.BitrateKbps > 0 {
		value := canonical.BitrateKbps
		bitrateKbps = &value
	}
	var width *int
	if canonical.Width > 0 {
		value := canonical.Width
		width = &value
	}
	var height *int
	if canonical.Height > 0 {
		value := canonical.Height
		height = &value
	}
	var fps *float32
	if canonical.FPS > 0 {
		value := float32(canonical.FPS)
		fps = &value
	}
	var audioChannels *int
	if canonical.AudioChannels > 0 {
		value := canonical.AudioChannels
		audioChannels = &value
	}
	var audioBitrateKbps *int
	if canonical.AudioBitrateKbps > 0 {
		value := canonical.AudioBitrateKbps
		audioBitrateKbps = &value
	}
	interlaced := canonical.Interlaced
	return &PlaybackSourceProfile{
		Container:        strPtr(canonical.Container),
		VideoCodec:       strPtr(canonical.VideoCodec),
		AudioCodec:       strPtr(canonical.AudioCodec),
		BitrateKbps:      bitrateKbps,
		Width:            width,
		Height:           height,
		Fps:              fps,
		Interlaced:       &interlaced,
		AudioChannels:    audioChannels,
		AudioBitrateKbps: audioBitrateKbps,
	}
}

func applySegmentTruth(info *PlaybackInfo, truth *hls.SegmentTruth, attempted bool) {
	isSeekable := !attempted
	canSeek := !attempted

	if truth != nil {
		isSeekable = true
		canSeek = true
		if truth.IsVOD {
			dur := int64(truth.TotalDuration.Seconds())
			info.DvrWindowSeconds = &dur
		} else if truth.HasPDT {
			start := truth.FirstPDT.Unix()
			edge := truth.LastPDT.Add(truth.LastDuration).Unix()
			window := edge - start
			if window > 0 {
				info.StartUnix = &start
				info.LiveEdgeUnix = &edge
				info.DvrWindowSeconds = &window
			} else {
				isSeekable = false
				canSeek = false
			}
		}
	}

	info.IsSeekable = &isSeekable
	info.Seekable = &canSeek
}
