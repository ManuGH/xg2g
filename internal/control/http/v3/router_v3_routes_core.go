// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerAuthRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodPost, "/auth/session", "CreateSession", wrapper.CreateSession)
}

func registerDVRRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/dvr/capabilities", "GetDvrCapabilities", wrapper.GetDvrCapabilities)
	register.add(http.MethodGet, "/dvr/status", "GetDvrStatus", wrapper.GetDvrStatus)
}

func registerEPGRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/epg", "GetEpg", wrapper.GetEpg)
}

func registerIntentRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodPost, "/intents", "CreateIntent", wrapper.CreateIntent)
}

func registerLogRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/logs", "GetLogs", wrapper.GetLogs)
}

func registerReceiverRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/receiver/current", "GetReceiverCurrent", wrapper.GetReceiverCurrent)
}
