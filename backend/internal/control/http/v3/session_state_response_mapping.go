// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	v3sessions "github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
)

func writeSessionStateResponse(w http.ResponseWriter, r *http.Request, hlsRoot string, result v3sessions.GetSessionResult) {
	resp := mapSessionStateResponse(requestID(r.Context()), hlsRoot, result)

	ensureTraceHeader(w, r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func mapSessionStateResponse(reqID string, hlsRoot string, result v3sessions.GetSessionResult) SessionResponse {
	session := result.Session
	out := result.Outcome

	resp := SessionResponse{
		SessionId:     openapi_types.UUID(parseUUID(session.SessionID)),
		ServiceRef:    &session.ServiceRef,
		Profile:       &session.Profile.Name,
		UpdatedAtMs:   toPtr(int(session.UpdatedAtUnix * 1000)),
		RequestId:     reqID,
		CorrelationId: &session.CorrelationID,
		Trace:         mapSessionPlaybackTrace(reqID, session, hlsRoot),
	}

	resp.State = mapSessionState(out.State)
	if reason, ok := mapSessionReason(out.Reason); ok {
		resp.Reason = &reason
	}
	detail := mapDetailCode(out.DetailCode)
	resp.ReasonDetail = &detail

	mode := result.PlaybackInfo.Mode
	if mode != "" {
		m := SessionResponseMode(mode)
		resp.Mode = &m
	}
	resp.DurationSeconds = toFloat32Ptr(result.PlaybackInfo.DurationSeconds)
	resp.SeekableStartSeconds = toFloat32Ptr(result.PlaybackInfo.SeekableStartSeconds)
	resp.SeekableEndSeconds = toFloat32Ptr(result.PlaybackInfo.SeekableEndSeconds)
	resp.LiveEdgeSeconds = toFloat32Ptr(result.PlaybackInfo.LiveEdgeSeconds)

	playbackURL := fmt.Sprintf("%s/sessions/%s/hls/index.m3u8", V3BaseURL, session.SessionID)
	resp.PlaybackUrl = &playbackURL

	return resp
}

func toPtr[T any](v T) *T {
	return &v
}

func toFloat32Ptr(v *float64) *float32 {
	if v == nil {
		return nil
	}
	f32 := float32(*v)
	return &f32
}

func parseUUID(s string) uuid.UUID {
	u, _ := uuid.Parse(s)
	return u
}
