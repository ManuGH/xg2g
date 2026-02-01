// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/authz"
	"github.com/go-chi/chi/v5"
)

// RouterOptions configures the handwritten v3 router.
type RouterOptions struct {
	BaseURL          string
	BaseRouter       chi.Router
	Middlewares      []MiddlewareFunc
	ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

// NewRouter registers v3 routes and injects scope policy per operation.
// This replaces generated routing to keep server_gen.go transport-only.
func NewRouter(si ServerInterface, options RouterOptions) http.Handler {
	r := options.BaseRouter
	if r == nil {
		r = chi.NewRouter()
	}
	if options.ErrorHandlerFunc == nil {
		options.ErrorHandlerFunc = defaultBindErrorHandler
	}

	wrapper := ServerInterfaceWrapper{
		Handler:            si,
		HandlerMiddlewares: options.Middlewares,
		ErrorHandlerFunc:   options.ErrorHandlerFunc,
	}

	register := func(method, path, opID string, handler http.HandlerFunc) {
		r.Method(method, options.BaseURL+path, withScopes(opID, handler))
	}

	register(http.MethodPost, "/auth/session", "CreateSession", wrapper.CreateSession)
	register(http.MethodGet, "/dvr/capabilities", "GetDvrCapabilities", wrapper.GetDvrCapabilities)
	register(http.MethodGet, "/dvr/status", "GetDvrStatus", wrapper.GetDvrStatus)
	register(http.MethodGet, "/epg", "GetEpg", wrapper.GetEpg)
	register(http.MethodPost, "/intents", "CreateIntent", wrapper.CreateIntent)
	register(http.MethodGet, "/logs", "GetLogs", wrapper.GetLogs)
	register(http.MethodGet, "/receiver/current", "GetReceiverCurrent", wrapper.GetReceiverCurrent)
	register(http.MethodGet, "/recordings", "GetRecordings", wrapper.GetRecordings)
	register(http.MethodDelete, "/recordings/{recordingId}", "DeleteRecording", wrapper.DeleteRecording)
	register(http.MethodGet, "/recordings/{recordingId}/playlist.m3u8", "GetRecordingHLSPlaylist", wrapper.GetRecordingHLSPlaylist)
	register(http.MethodHead, "/recordings/{recordingId}/playlist.m3u8", "GetRecordingHLSPlaylistHead", wrapper.GetRecordingHLSPlaylistHead)
	register(http.MethodGet, "/recordings/{recordingId}/status", "GetRecordingsRecordingIdStatus", wrapper.GetRecordingsRecordingIdStatus)
	register(http.MethodGet, "/recordings/{recordingId}/stream-info", "GetRecordingPlaybackInfo", wrapper.GetRecordingPlaybackInfo)
	register(http.MethodPost, "/recordings/{recordingId}/stream-info", "PostRecordingPlaybackInfo", wrapper.PostRecordingPlaybackInfo)
	register(http.MethodGet, "/recordings/{recordingId}/stream.mp4", "StreamRecordingDirect", wrapper.StreamRecordingDirect)
	register(http.MethodHead, "/recordings/{recordingId}/stream.mp4", "ProbeRecordingMp4", wrapper.ProbeRecordingMp4)
	register(http.MethodGet, "/recordings/{recordingId}/timeshift.m3u8", "GetRecordingHLSTimeshift", wrapper.GetRecordingHLSTimeshift)
	register(http.MethodHead, "/recordings/{recordingId}/timeshift.m3u8", "GetRecordingHLSTimeshiftHead", wrapper.GetRecordingHLSTimeshiftHead)
	register(http.MethodGet, "/recordings/{recordingId}/{segment}", "GetRecordingHLSCustomSegment", wrapper.GetRecordingHLSCustomSegment)
	register(http.MethodHead, "/recordings/{recordingId}/{segment}", "GetRecordingHLSCustomSegmentHead", wrapper.GetRecordingHLSCustomSegmentHead)
	register(http.MethodGet, "/series-rules", "GetSeriesRules", wrapper.GetSeriesRules)
	register(http.MethodPost, "/series-rules", "CreateSeriesRule", wrapper.CreateSeriesRule)
	register(http.MethodPost, "/series-rules/run", "RunAllSeriesRules", wrapper.RunAllSeriesRules)
	register(http.MethodDelete, "/series-rules/{id}", "DeleteSeriesRule", wrapper.DeleteSeriesRule)
	register(http.MethodPut, "/series-rules/{id}", "UpdateSeriesRule", wrapper.UpdateSeriesRule)
	register(http.MethodPost, "/series-rules/{id}/run", "RunSeriesRule", wrapper.RunSeriesRule)
	register(http.MethodGet, "/services", "GetServices", wrapper.GetServices)
	register(http.MethodGet, "/services/bouquets", "GetServicesBouquets", wrapper.GetServicesBouquets)
	register(http.MethodPost, "/services/now-next", "PostServicesNowNext", wrapper.PostServicesNowNext)
	register(http.MethodPost, "/services/{id}/toggle", "PostServicesIdToggle", wrapper.PostServicesIdToggle)
	register(http.MethodGet, "/sessions", "ListSessions", wrapper.ListSessions)
	register(http.MethodGet, "/sessions/{sessionID}", "GetSessionState", wrapper.GetSessionState)
	register(http.MethodGet, "/sessions/{sessionID}/hls/{filename}", "ServeHLS", wrapper.ServeHLS)
	register(http.MethodHead, "/sessions/{sessionID}/hls/{filename}", "ServeHLSHead", wrapper.ServeHLSHead)
	register(http.MethodPost, "/sessions/{sessionId}/feedback", "ReportPlaybackFeedback", wrapper.ReportPlaybackFeedback)
	register(http.MethodGet, "/streams", "GetStreams", wrapper.GetStreams)
	register(http.MethodDelete, "/streams/{id}", "DeleteStreamsId", wrapper.DeleteStreamsId)
	register(http.MethodGet, "/system/config", "GetSystemConfig", wrapper.GetSystemConfig)
	register(http.MethodPut, "/system/config", "PutSystemConfig", wrapper.PutSystemConfig)
	register(http.MethodGet, "/system/health", "GetSystemHealth", wrapper.GetSystemHealth)
	register(http.MethodGet, "/system/healthz", "GetSystemHealthz", wrapper.GetSystemHealthz)
	register(http.MethodGet, "/system/info", "GetSystemInfo", wrapper.GetSystemInfo)
	register(http.MethodPost, "/system/refresh", "PostSystemRefresh", wrapper.PostSystemRefresh)
	register(http.MethodGet, "/system/scan", "GetSystemScanStatus", wrapper.GetSystemScanStatus)
	register(http.MethodPost, "/system/scan", "TriggerSystemScan", wrapper.TriggerSystemScan)
	register(http.MethodGet, "/timers", "GetTimers", wrapper.GetTimers)
	register(http.MethodPost, "/timers", "AddTimer", wrapper.AddTimer)
	register(http.MethodPost, "/timers/conflicts:preview", "PreviewConflicts", wrapper.PreviewConflicts)
	register(http.MethodDelete, "/timers/{timerId}", "DeleteTimer", wrapper.DeleteTimer)
	register(http.MethodGet, "/timers/{timerId}", "GetTimer", wrapper.GetTimer)
	register(http.MethodPatch, "/timers/{timerId}", "UpdateTimer", wrapper.UpdateTimer)

	return r
}

func withScopes(operationID string, next http.Handler) http.Handler {
	scopes := authz.MustScopes(operationID)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), bearerAuthScopesKey, scopes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func defaultBindErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request", "INVALID_INPUT", err.Error(), nil)
}
