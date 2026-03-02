// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

// PostLivePlaybackInfo implements ServerInterface.
func (s *Server) PostLivePlaybackInfo(w http.ResponseWriter, r *http.Request) {
	var req LivePlaybackInfoRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		metrics.IncPlaybackError(playbackSchemaLiveLabel, playbackStagePlaybackInfoLabel, "INVALID_CAPABILITIES")
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_CAPABILITIES", "Failed to parse live playback body: "+err.Error(), nil)
		return
	}

	serviceRef := strings.TrimSpace(req.ServiceRef)
	if serviceRef == "" {
		metrics.IncPlaybackError(playbackSchemaLiveLabel, playbackStagePlaybackInfoLabel, "INVALID_INPUT")
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_INPUT", "serviceRef is required", nil)
		return
	}

	if req.Capabilities.CapabilitiesVersion < 1 {
		metrics.IncPlaybackError(playbackSchemaLiveLabel, playbackStagePlaybackInfoLabel, "INVALID_CAPABILITIES")
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_CAPABILITIES", "capabilities_version must be >= 1", nil)
		return
	}

	s.handleLivePlaybackInfo(w, r, serviceRef, &req.Capabilities)
}

func (s *Server) handleLivePlaybackInfo(w http.ResponseWriter, r *http.Request, serviceRef string, caps *PlaybackCapabilities) {
	deps := s.sessionsModuleDeps()
	cfg := deps.cfg

	if u, ok := platformnet.ParseDirectHTTPURL(serviceRef); ok {
		normalized, err := platformnet.ValidateOutboundURL(r.Context(), u.String(), outboundPolicyFromConfig(cfg))
		if err != nil {
			metrics.IncPlaybackError(playbackSchemaLiveLabel, playbackStagePlaybackInfoLabel, "INVALID_INPUT")
			writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_INPUT", "direct URL serviceRef rejected by outbound policy", nil)
			return
		}
		serviceRef = normalized
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	var clientCaps *capabilities.PlaybackCapabilities
	if caps != nil {
		c := mapV3CapsToInternal(caps)
		clientCaps = &c
	}

	principal := ""
	if p := auth.PrincipalFromContext(r.Context()); p != nil {
		principal = p.ID
	}
	resolvedCaps := recordings.ResolveCapabilities(r.Context(), principal, "v3.1", "", headers, clientCaps)

	serverCanTranscode := cfg.FFmpeg.Bin != "" && cfg.HLS.Root != ""
	clientAllowsTranscode := resolvedCaps.AllowTranscode == nil || *resolvedCaps.AllowTranscode
	allowTranscode := serverCanTranscode && clientAllowsTranscode

	source := deriveLiveSourceTruth(serviceRef, deps.channelScanner)

	input := decision.DecisionInput{
		RequestID:    log.RequestIDFromContext(r.Context()),
		APIVersion:   "v3.1",
		Source:       source,
		Capabilities: decision.FromCapabilities(resolvedCaps),
		Policy: decision.Policy{
			AllowTranscode: allowTranscode,
		},
	}

	_, decOut, prob := decision.Decide(r.Context(), input, "compact")
	if prob != nil {
		metrics.IncPlaybackError(playbackSchemaLiveLabel, playbackStagePlaybackInfoLabel, prob.Code)
		writeProblem(w, r, prob.Status, prob.Type, prob.Title, prob.Code, prob.Detail, nil)
		return
	}

	dto := mapLivePlaybackInfo(serviceRef, decOut, caps, source)
	if dto.RequestId == "" {
		dto.RequestId = requestID(r.Context())
		if dto.Decision != nil && dto.Decision.Trace.RequestId == "" {
			dto.Decision.Trace.RequestId = dto.RequestId
		}
	}
	if dto.Mode != PlaybackInfoModeDeny {
		decisionToken := s.attestLivePlaybackDecision(dto.RequestId, principal, serviceRef, string(dto.Mode))
		if decisionToken == "" {
			metrics.IncPlaybackError(playbackSchemaLiveLabel, playbackStagePlaybackInfoLabel, "ATTESTATION_UNAVAILABLE")
			writeProblem(w, r, http.StatusServiceUnavailable, "recordings/unavailable", "Service Unavailable", "ATTESTATION_UNAVAILABLE", "live playback attestation unavailable", nil)
			return
		}
		dto.PlaybackDecisionToken = &decisionToken
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto)
}

func mapLivePlaybackInfo(serviceRef string, dec *decision.Decision, caps *PlaybackCapabilities, source decision.Source) PlaybackInfo {
	proto := decision.ProtocolFrom(dec)
	mode := derivePlaybackInfoMode(dec, caps, proto)

	reasonStrings := decision.ReasonsAsStrings(dec, nil)
	if mode == PlaybackInfoModeDeny && (dec == nil || dec.Mode != decision.ModeDeny) {
		reasonStrings = prependReasonIfMissing(reasonStrings, string(decision.ReasonNoCompatiblePlaybackPath))
	}
	primaryStr := decision.ReasonPrimaryFrom(dec, nil)
	if mode == PlaybackInfoModeDeny && (dec == nil || dec.Mode != decision.ModeDeny) {
		primaryStr = string(decision.ReasonNoCompatiblePlaybackPath)
	}
	mainReason := PlaybackInfoReason(primaryStr)

	requestID := ""
	if dec != nil {
		requestID = dec.Trace.RequestID
	}
	sessionID := fmt.Sprintf("live:%s", serviceRef)

	var decDTO PlaybackDecision
	if dec != nil {
		decDTO.Mode = PlaybackDecisionMode(dec.Mode)
		decDTO.Selected.Container = dec.Selected.Container
		decDTO.Selected.VideoCodec = dec.Selected.VideoCodec
		decDTO.Selected.AudioCodec = dec.Selected.AudioCodec
		decDTO.Constraints = append(decDTO.Constraints, dec.Constraints...)

		selectedKind := strings.TrimSpace(string(dec.SelectedOutputKind))
		if mode == PlaybackInfoModeDeny {
			selectedKind = ""
		}
		if selectedKind != "" {
			kind := PlaybackDecisionSelectedOutputKind(selectedKind)
			decDTO.SelectedOutputKind = &kind
		}
		decDTO.Trace.RequestId = dec.Trace.RequestID
	}

	decDTO.Trace.SessionId = &sessionID
	decDTO.Reasons = append([]string{}, reasonStrings...)
	if decDTO.Reasons == nil {
		decDTO.Reasons = []string{}
	}
	if decDTO.Outputs == nil {
		decDTO.Outputs = []PlaybackOutput{}
	}
	if decDTO.Constraints == nil {
		decDTO.Constraints = []string{}
	}

	container := source.Container
	videoCodec := source.VideoCodec
	audioCodec := source.AudioCodec

	return PlaybackInfo{
		Mode:       mode,
		Container:  &container,
		VideoCodec: &videoCodec,
		AudioCodec: &audioCodec,
		Reason:     &mainReason,
		Decision:   &decDTO,
		RequestId:  requestID,
		SessionId:  sessionID,
	}
}

func deriveLiveSourceTruth(serviceRef string, scanner ChannelScanner) decision.Source {
	source := decision.Source{
		Container:  "mpegts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}

	if scanner == nil {
		return source
	}

	cap, ok := scanner.GetCapability(serviceRef)
	if !ok {
		return source
	}

	if codec := normalizeLiveVideoCodec(cap.Codec); codec != "" {
		source.VideoCodec = codec
	}

	if w, h := parseLiveResolution(cap.Resolution); w > 0 && h > 0 {
		source.Width = w
		source.Height = h
	}

	return source
}

func normalizeLiveVideoCodec(raw string) string {
	codec := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(codec, "hevc"), strings.Contains(codec, "h265"):
		return "hevc"
	case strings.Contains(codec, "h264"), strings.Contains(codec, "avc"):
		return "h264"
	case strings.Contains(codec, "mpeg2"):
		return "mpeg2"
	case strings.Contains(codec, "av1"):
		return "av1"
	default:
		return ""
	}
}

func parseLiveResolution(raw string) (int, int) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(raw)), "x")
	if len(parts) != 2 {
		return 0, 0
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0
	}
	return w, h
}
