// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerSeriesRoutes(register routeRegistrar, handler seriesRoutes) {
	register.add(http.MethodGet, "/series-rules", "GetSeriesRules", handler.GetSeriesRules)
	register.add(http.MethodPost, "/series-rules", "CreateSeriesRule", handler.CreateSeriesRule)
	register.add(http.MethodPost, "/series-rules/run", "RunAllSeriesRules", handler.RunAllSeriesRules)
	register.add(http.MethodDelete, "/series-rules/{id}", "DeleteSeriesRule", handler.DeleteSeriesRule)
	register.add(http.MethodPut, "/series-rules/{id}", "UpdateSeriesRule", handler.UpdateSeriesRule)
	register.add(http.MethodPost, "/series-rules/{id}/run", "RunSeriesRule", handler.RunSeriesRule)
}

func registerServiceRoutes(register routeRegistrar, handler serviceRoutes) {
	register.add(http.MethodGet, "/services", "GetServices", handler.GetServices)
	register.add(http.MethodGet, "/services/bouquets", "GetServicesBouquets", handler.GetServicesBouquets)
	register.add(http.MethodPost, "/services/now-next", "PostServicesNowNext", handler.PostServicesNowNext)
	register.add(http.MethodPost, "/services/{id}/toggle", "PostServicesIdToggle", handler.PostServicesIdToggle)
}
