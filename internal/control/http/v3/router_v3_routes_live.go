// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerLivePlaybackRoutes(register routeRegistrar, handler livePlaybackRoutes) {
	register.add(http.MethodPost, "/live/stream-info", "PostLivePlaybackInfo", handler.PostLivePlaybackInfo)
}
