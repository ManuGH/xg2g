package v3

import (
	"context"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/read"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func (s *Server) householdServiceSnapshot() *household.Service {
	s.mu.RLock()
	service := s.householdService
	s.mu.RUnlock()
	return service
}

func (s *Server) currentHouseholdProfile(ctx context.Context) household.Profile {
	if profile := household.ProfileFromContext(ctx); profile != nil {
		return household.CloneProfile(*profile)
	}
	if service := s.householdServiceSnapshot(); service != nil {
		if resolved, err := service.Resolve(ctx, ""); err == nil {
			return resolved
		}
	}
	return household.CreateDefaultProfile()
}

func (s *Server) requireHouseholdDVRPlaybackAccess(w http.ResponseWriter, r *http.Request) (household.Profile, bool) {
	profile := s.currentHouseholdProfile(r.Context())
	if household.CanAccessDVRPlayback(profile) {
		return profile, true
	}
	writeHouseholdForbidden(w, r, "household/dvr_playback_forbidden", "DVR Playback Forbidden", "The active household profile is not allowed to watch recordings")
	return household.Profile{}, false
}

func (s *Server) requireHouseholdDVRManageAccess(w http.ResponseWriter, r *http.Request) (household.Profile, bool) {
	profile := s.currentHouseholdProfile(r.Context())
	if household.CanManageDVR(profile) {
		return profile, true
	}
	writeHouseholdForbidden(w, r, "household/dvr_manage_forbidden", "DVR Control Forbidden", "The active household profile is not allowed to manage DVR features")
	return household.Profile{}, false
}

func (s *Server) requireHouseholdSettingsAccess(w http.ResponseWriter, r *http.Request) (household.Profile, bool) {
	profile := s.currentHouseholdProfile(r.Context())
	if household.CanAccessSettings(profile) {
		return profile, true
	}
	writeHouseholdForbidden(w, r, "household/settings_forbidden", "Settings Forbidden", "The active household profile is not allowed to access settings")
	return household.Profile{}, false
}

func (s *Server) requireHouseholdRecordingAccess(w http.ResponseWriter, r *http.Request, recordingID string) (household.Profile, bool) {
	profile, ok := s.requireHouseholdDVRPlaybackAccess(w, r)
	if !ok {
		return household.Profile{}, false
	}
	profile = household.NormalizeProfile(profile)

	serviceRef, decoded := recservice.DecodeRecordingID(recordingID)
	if !decoded {
		return profile, true
	}
	if household.IsServiceAllowedNormalized(profile, serviceRef, "") {
		return profile, true
	}

	writeHouseholdForbidden(w, r, "household/recording_forbidden", "Recording Forbidden", "The active household profile is not allowed to access this recording")
	return household.Profile{}, false
}

func (s *Server) requireHouseholdTimerServiceAccess(w http.ResponseWriter, r *http.Request, serviceRef string) (household.Profile, bool) {
	profile, ok := s.requireHouseholdDVRManageAccess(w, r)
	if !ok {
		return household.Profile{}, false
	}
	profile = household.NormalizeProfile(profile)
	if household.IsServiceAllowedNormalized(profile, serviceRef, "") {
		return profile, true
	}

	writeHouseholdForbidden(w, r, "household/timer_forbidden", "Timer Forbidden", "The active household profile is not allowed to manage DVR entries for this service")
	return household.Profile{}, false
}

func writeHouseholdForbidden(w http.ResponseWriter, r *http.Request, problemType, title, detail string) {
	writeRegisteredProblem(w, r, http.StatusForbidden, problemType, title, problemcode.CodeForbidden, detail, nil)
}

func (s *Server) householdVisibleServices(profile household.Profile, deps systemModuleDeps) ([]read.Service, error) {
	res, err := read.GetServices(deps.cfg, deps.snap, deps.servicesSource, read.ServicesQuery{})
	if err != nil {
		return nil, err
	}
	if len(res.Items) == 0 {
		return []read.Service{}, nil
	}

	profile = household.NormalizeProfile(profile)
	visible := make([]read.Service, 0, len(res.Items))
	for _, item := range res.Items {
		if household.IsServiceAllowedNormalized(profile, item.ServiceRef, item.Group) {
			visible = append(visible, item)
		}
	}
	return visible, nil
}

func (s *Server) householdVisibleServiceRefSet(profile household.Profile, deps systemModuleDeps) (map[string]struct{}, error) {
	profile = household.NormalizeProfile(profile)
	if !household.HasServiceRestrictionsNormalized(profile) {
		return nil, nil
	}

	visible, err := s.householdVisibleServices(profile, deps)
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]struct{}, len(visible))
	for _, item := range visible {
		if item.ServiceRef == "" {
			continue
		}
		allowed[item.ServiceRef] = struct{}{}
	}
	return allowed, nil
}
