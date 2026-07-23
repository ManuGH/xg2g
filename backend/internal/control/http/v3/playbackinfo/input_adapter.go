// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackinfo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type LivePlaybackInfoInput struct {
	ServiceRef   string
	Capabilities *PlaybackCapabilities
}

func ParseRecordingPlaybackPostInput(r *http.Request) (*PlaybackCapabilities, *PlaybackInfoInputProblem) {
	var caps PlaybackCapabilities
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&caps); err != nil {
		return nil, &PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "recordings/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidCapabilities,
			Detail:      "Failed to parse capabilities body: " + err.Error(),
		}
	}
	if caps.CapabilitiesVersion < 1 {
		return nil, &PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "recordings/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidCapabilities,
			Detail:      "capabilities_version must be >= 1",
		}
	}
	return &caps, nil
}

func ParseLivePlaybackPostInput(r *http.Request) (LivePlaybackInfoInput, *PlaybackInfoInputProblem) {
	bodyBytes, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		return LivePlaybackInfoInput{}, &PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "live/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidInput,
			Detail:      "Failed to read request body: " + readErr.Error(),
		}
	}

	rawBody := string(bodyBytes)
	requestContext := v3recordings.NormalizePlaybackInfoRequestContext(r.Header.Get(v3recordings.PlaybackInfoContextHeader))
	bodyLog := log.L().Debug()
	if requestContext != "" {
		bodyLog = bodyLog.Str("request_context", requestContext)
	}
	if requestContext == v3recordings.PlaybackInfoContextEpgBadge {
		bodyLog.Msg("PostLivePlaybackInfo preview request body omitted")
	} else {
		bodyLog.Str("body", rawBody).Msg("PostLivePlaybackInfo request body")
	}

	var req PostLivePlaybackInfoJSONRequestBody
	dec := json.NewDecoder(strings.NewReader(rawBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		parseLog := log.L().Warn().Err(err).Str("body", rawBody)
		if requestContext != "" {
			parseLog = parseLog.Str("request_context", requestContext)
		}
		parseLog.Msg("PostLivePlaybackInfo parse failed")
		return LivePlaybackInfoInput{}, &PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "live/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidInput,
			Detail:      "Failed to parse request body: " + err.Error(),
		}
	}
	summary := log.L().Debug().
		Str("service_ref", normalize.ServiceRef(req.ServiceRef)).
		Str("request_context", requestContext).
		Int("capabilities_version", req.Capabilities.CapabilitiesVersion).
		Str("client_family_fallback", strings.TrimSpace(valueOrEmpty(req.Capabilities.ClientFamilyFallback))).
		Str("preferred_hls_engine", strings.TrimSpace(valueOrEmpty(req.Capabilities.PreferredHlsEngine))).
		Str("device_type", strings.TrimSpace(valueOrEmpty(req.Capabilities.DeviceType))).
		Str("containers", strings.Join(req.Capabilities.Container, ",")).
		Str("video_codecs", strings.Join(req.Capabilities.VideoCodecs, ",")).
		Str("audio_codecs", strings.Join(req.Capabilities.AudioCodecs, ",")).
		Str("hls_engines", strings.Join(valueSliceOrEmpty(req.Capabilities.HlsEngines), ","))
	if req.Capabilities.RuntimeProbeUsed != nil {
		summary = summary.Bool("runtime_probe_used", *req.Capabilities.RuntimeProbeUsed)
	}
	if req.Capabilities.RuntimeProbeVersion != nil {
		summary = summary.Int("runtime_probe_version", *req.Capabilities.RuntimeProbeVersion)
	}
	if req.Capabilities.AllowTranscode != nil {
		summary = summary.Bool("allow_transcode", *req.Capabilities.AllowTranscode)
	}
	if requestContext == v3recordings.PlaybackInfoContextEpgBadge {
		summary.Msg("PostLivePlaybackInfo preview capability summary")
	} else {
		summary.Msg("PostLivePlaybackInfo capability summary")
	}
	if req.ServiceRef == "" {
		return LivePlaybackInfoInput{}, &PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "live/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidInput,
			Detail:      "serviceRef is required",
		}
	}

	serviceRef := normalize.ServiceRef(req.ServiceRef)
	if err := recordings.ValidateLiveRef(serviceRef); err != nil {
		return LivePlaybackInfoInput{}, &PlaybackInfoInputProblem{
			Status:      http.StatusBadRequest,
			ProblemType: "live/invalid",
			Title:       "Invalid Request",
			Code:        problemcode.CodeInvalidInput,
			Detail:      "serviceRef must be a valid live Enigma2 reference",
		}
	}

	return LivePlaybackInfoInput{
		ServiceRef:   serviceRef,
		Capabilities: (*PlaybackCapabilities)(&req.Capabilities),
	}, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func valueSliceOrEmpty(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}
