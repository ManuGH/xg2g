// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerSessionRoutes(register routeRegistrar, handler sessionRoutes) {
	register.add(http.MethodGet, "/sessions", "ListSessions", handler.ListSessions)
	register.add(http.MethodGet, "/sessions/{sessionID}", "GetSessionState", handler.GetSessionState)
	register.add(http.MethodGet, "/sessions/{sessionID}/hls/{filename}", "ServeHLS", handler.ServeHLS)
	register.add(http.MethodHead, "/sessions/{sessionID}/hls/{filename}", "ServeHLSHead", handler.ServeHLSHead)
	register.add(http.MethodPost, "/sessions/{sessionId}/feedback", "ReportPlaybackFeedback", handler.ReportPlaybackFeedback)
}

func registerStreamRoutes(register routeRegistrar, handler streamRoutes) {
	register.add(http.MethodGet, "/streams", "GetStreams", handler.GetStreams)
	register.add(http.MethodDelete, "/streams/{id}", "DeleteStreamsId", handler.DeleteStreamsId)
	register.add(http.MethodPost, "/live/stream-info", "PostLivePlaybackInfo", handler.PostLivePlaybackInfo)
}
