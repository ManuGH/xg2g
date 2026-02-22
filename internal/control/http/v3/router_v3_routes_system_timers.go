// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerConfigRoutes(register routeRegistrar, handler configRoutes) {
	register.add(http.MethodGet, "/system/config", "GetSystemConfig", handler.GetSystemConfig)
	register.add(http.MethodPut, "/system/config", "PutSystemConfig", handler.PutSystemConfig)
}

func registerSystemRoutes(register routeRegistrar, handler systemRoutes) {
	register.add(http.MethodGet, "/system/health", "GetSystemHealth", handler.GetSystemHealth)
	register.add(http.MethodGet, "/system/healthz", "GetSystemHealthz", handler.GetSystemHealthz)
	register.add(http.MethodGet, "/system/info", "GetSystemInfo", handler.GetSystemInfo)
	register.add(http.MethodPost, "/system/refresh", "PostSystemRefresh", handler.PostSystemRefresh)
	register.add(http.MethodGet, "/system/scan", "GetSystemScanStatus", handler.GetSystemScanStatus)
	register.add(http.MethodPost, "/system/scan", "TriggerSystemScan", handler.TriggerSystemScan)
}

func registerTimerRoutes(register routeRegistrar, handler timerRoutes) {
	register.add(http.MethodGet, "/timers", "GetTimers", handler.GetTimers)
	register.add(http.MethodPost, "/timers", "AddTimer", handler.AddTimer)
	register.add(http.MethodPost, "/timers/conflicts:preview", "PreviewConflicts", handler.PreviewConflicts)
	register.add(http.MethodDelete, "/timers/{timerId}", "DeleteTimer", handler.DeleteTimer)
	register.add(http.MethodGet, "/timers/{timerId}", "GetTimer", handler.GetTimer)
	register.add(http.MethodPatch, "/timers/{timerId}", "UpdateTimer", handler.UpdateTimer)
}
