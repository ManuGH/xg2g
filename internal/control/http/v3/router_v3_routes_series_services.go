// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "net/http"

func registerSeriesRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/series-rules", "GetSeriesRules", wrapper.GetSeriesRules)
	register.add(http.MethodPost, "/series-rules", "CreateSeriesRule", wrapper.CreateSeriesRule)
	register.add(http.MethodPost, "/series-rules/run", "RunAllSeriesRules", wrapper.RunAllSeriesRules)
	register.add(http.MethodDelete, "/series-rules/{id}", "DeleteSeriesRule", wrapper.DeleteSeriesRule)
	register.add(http.MethodPut, "/series-rules/{id}", "UpdateSeriesRule", wrapper.UpdateSeriesRule)
	register.add(http.MethodPost, "/series-rules/{id}/run", "RunSeriesRule", wrapper.RunSeriesRule)
}

func registerServiceRoutes(register routeRegistrar, wrapper ServerInterfaceWrapper) {
	register.add(http.MethodGet, "/services", "GetServices", wrapper.GetServices)
	register.add(http.MethodGet, "/services/bouquets", "GetServicesBouquets", wrapper.GetServicesBouquets)
	register.add(http.MethodPost, "/services/now-next", "PostServicesNowNext", wrapper.PostServicesNowNext)
	register.add(http.MethodPost, "/services/{id}/toggle", "PostServicesIdToggle", wrapper.PostServicesIdToggle)
}
