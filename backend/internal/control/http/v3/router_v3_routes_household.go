package v3

import "net/http"

func registerHouseholdRoutes(register routeRegistrar, handler householdRoutes) {
	register.add(http.MethodGet, "/household/unlock", "GetHouseholdUnlock", handler.GetHouseholdUnlock)
	register.add(http.MethodGet, "/household/profiles", "GetHouseholdProfiles", handler.GetHouseholdProfiles)
	register.add(http.MethodPost, "/household/unlock", "PostHouseholdUnlock", handler.PostHouseholdUnlock)
	register.add(http.MethodPost, "/household/profiles", "PostHouseholdProfiles", handler.PostHouseholdProfiles)
	register.add(http.MethodDelete, "/household/unlock", "DeleteHouseholdUnlock", handler.DeleteHouseholdUnlock)
	register.add(http.MethodPut, "/household/profiles/{profileId}", "PutHouseholdProfile", handler.PutHouseholdProfile)
	register.add(http.MethodDelete, "/household/profiles/{profileId}", "DeleteHouseholdProfile", handler.DeleteHouseholdProfile)
}
