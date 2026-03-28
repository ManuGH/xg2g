// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/log"
)

func buildPlaybackInfoServiceRequest(r *http.Request, subjectID string, caps *PlaybackCapabilities, apiVersion string, schemaType string) v3recordings.PlaybackInfoRequest {
	return v3recordings.PlaybackInfoRequest{
		SubjectID:        subjectID,
		SubjectKind:      playbackSubjectKindForSchema(schemaType),
		APIVersion:       apiVersion,
		SchemaType:       schemaType,
		RequestedProfile: r.URL.Query().Get("profile"),
		PrincipalID:      playbackRequestPrincipalID(r),
		RequestID:        log.RequestIDFromContext(r.Context()),
		ClientProfile:    string(detectClientProfile(r)),
		Headers:          playbackRequestHeaders(r.Header),
		Capabilities:     mapV3CapsToInternal(caps),
	}
}

func playbackSubjectKindForSchema(schemaType string) v3recordings.PlaybackSubjectKind {
	if schemaType == "live" {
		return v3recordings.PlaybackSubjectLive
	}
	return v3recordings.PlaybackSubjectRecording
}

func playbackRequestPrincipalID(r *http.Request) string {
	if principal := auth.PrincipalFromContext(r.Context()); principal != nil {
		return principal.ID
	}
	return ""
}

func playbackRequestHeaders(headers http.Header) map[string]string {
	requestHeaders := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			requestHeaders[key] = values[0]
		}
	}
	return requestHeaders
}

func mapV3CapsToInternal(v3 *PlaybackCapabilities) *capabilities.PlaybackCapabilities {
	if v3 == nil {
		return nil
	}

	c := capabilities.PlaybackCapabilities{
		CapabilitiesVersion: v3.CapabilitiesVersion,
		Containers:          v3.Container,
		VideoCodecs:         v3.VideoCodecs,
		AudioCodecs:         v3.AudioCodecs,
		SupportsHLS:         false,
	}
	if v3.VideoCodecSignals != nil {
		c.VideoCodecSignals = make([]capabilities.VideoCodecSignal, 0, len(*v3.VideoCodecSignals))
		for _, signal := range *v3.VideoCodecSignals {
			mapped := capabilities.VideoCodecSignal{
				Codec:     signal.Codec,
				Supported: signal.Supported,
			}
			if signal.Smooth != nil {
				v := *signal.Smooth
				mapped.Smooth = &v
			}
			if signal.PowerEfficient != nil {
				v := *signal.PowerEfficient
				mapped.PowerEfficient = &v
			}
			c.VideoCodecSignals = append(c.VideoCodecSignals, mapped)
		}
	}
	if v3.SupportsHls != nil {
		c.SupportsHLS = *v3.SupportsHls
		c.SupportsHLSExplicit = true
	}
	if v3.HlsEngines != nil {
		c.HLSEngines = append([]string(nil), (*v3.HlsEngines)...)
		if len(c.HLSEngines) > 0 && v3.SupportsHls == nil {
			c.SupportsHLS = true
		}
	}
	if v3.PreferredHlsEngine != nil {
		c.PreferredHLSEngine = *v3.PreferredHlsEngine
	}
	if v3.RuntimeProbeUsed != nil {
		c.RuntimeProbeUsed = *v3.RuntimeProbeUsed
	}
	if v3.RuntimeProbeVersion != nil {
		c.RuntimeProbeVersion = *v3.RuntimeProbeVersion
	}
	if v3.ClientFamilyFallback != nil {
		c.ClientFamilyFallback = *v3.ClientFamilyFallback
	}
	c.SupportsRange = v3.SupportsRange
	c.AllowTranscode = v3.AllowTranscode
	if v3.MaxVideo != nil {
		c.MaxVideo = &capabilities.MaxVideo{
			Width:  derefInt(v3.MaxVideo.Width),
			Height: derefInt(v3.MaxVideo.Height),
			Fps:    derefInt(v3.MaxVideo.Fps),
		}
	}
	if v3.DeviceType != nil {
		c.DeviceType = *v3.DeviceType
	}
	return &c
}
