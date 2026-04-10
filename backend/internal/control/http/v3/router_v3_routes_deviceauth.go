// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerDeviceAuthRoutes(register routeRegistrar, handler deviceAuthRoutes) {
	register.add(http.MethodPost, "/auth/device/session", "CreateDeviceSession", handler.CreateDeviceSession)
	register.add(http.MethodPost, "/auth/web-bootstrap", "CreateWebBootstrap", handler.CreateWebBootstrap)
	register.add(http.MethodGet, "/auth/web-bootstrap/{bootstrapId}", "CompleteWebBootstrap", handler.CompleteWebBootstrap)
}
