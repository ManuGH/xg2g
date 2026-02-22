// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerAuthRoutes(register routeRegistrar, handler authRoutes) {
	register.add(http.MethodPost, "/auth/session", "CreateSession", handler.CreateSession)
}

func registerDVRRoutes(register routeRegistrar, handler dvrRoutes) {
	register.add(http.MethodGet, "/dvr/capabilities", "GetDvrCapabilities", handler.GetDvrCapabilities)
	register.add(http.MethodGet, "/dvr/status", "GetDvrStatus", handler.GetDvrStatus)
}

func registerEPGRoutes(register routeRegistrar, handler epgRoutes) {
	register.add(http.MethodGet, "/epg", "GetEpg", handler.GetEpg)
}

func registerIntentRoutes(register routeRegistrar, handler intentRoutes) {
	register.add(http.MethodPost, "/intents", "CreateIntent", handler.CreateIntent)
}

func registerLogRoutes(register routeRegistrar, handler logRoutes) {
	register.add(http.MethodGet, "/logs", "GetLogs", handler.GetLogs)
}

func registerReceiverRoutes(register routeRegistrar, handler receiverRoutes) {
	register.add(http.MethodGet, "/receiver/current", "GetReceiverCurrent", handler.GetReceiverCurrent)
}
