// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerSessionRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/sessions", "ListSessions", wrapper.ListSessions)
	register.add(http.MethodGet, "/sessions/{sessionID}", "GetSessionState", wrapper.GetSessionState)
	register.add(http.MethodGet, "/sessions/{sessionID}/hls/{filename}", "ServeHLS", wrapper.ServeHLS)
	register.add(http.MethodHead, "/sessions/{sessionID}/hls/{filename}", "ServeHLSHead", wrapper.ServeHLSHead)
	register.add(http.MethodPost, "/sessions/{sessionId}/feedback", "ReportPlaybackFeedback", wrapper.ReportPlaybackFeedback)
}

func registerStreamRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/streams", "GetStreams", wrapper.GetStreams)
	register.add(http.MethodDelete, "/streams/{id}", "DeleteStreamsId", wrapper.DeleteStreamsId)
}
