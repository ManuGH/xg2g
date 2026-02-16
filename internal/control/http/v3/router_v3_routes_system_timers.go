// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerSystemRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/system/config", "GetSystemConfig", wrapper.GetSystemConfig)
	register.add(http.MethodPut, "/system/config", "PutSystemConfig", wrapper.PutSystemConfig)
	register.add(http.MethodGet, "/system/health", "GetSystemHealth", wrapper.GetSystemHealth)
	register.add(http.MethodGet, "/system/healthz", "GetSystemHealthz", wrapper.GetSystemHealthz)
	register.add(http.MethodGet, "/system/info", "GetSystemInfo", wrapper.GetSystemInfo)
	register.add(http.MethodPost, "/system/refresh", "PostSystemRefresh", wrapper.PostSystemRefresh)
	register.add(http.MethodGet, "/system/scan", "GetSystemScanStatus", wrapper.GetSystemScanStatus)
	register.add(http.MethodPost, "/system/scan", "TriggerSystemScan", wrapper.TriggerSystemScan)
}

func registerTimerRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/timers", "GetTimers", wrapper.GetTimers)
	register.add(http.MethodPost, "/timers", "AddTimer", wrapper.AddTimer)
	register.add(http.MethodPost, "/timers/conflicts:preview", "PreviewConflicts", wrapper.PreviewConflicts)
	register.add(http.MethodDelete, "/timers/{timerId}", "DeleteTimer", wrapper.DeleteTimer)
	register.add(http.MethodGet, "/timers/{timerId}", "GetTimer", wrapper.GetTimer)
	register.add(http.MethodPatch, "/timers/{timerId}", "UpdateTimer", wrapper.UpdateTimer)
}
