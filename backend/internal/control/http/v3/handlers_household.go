package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	householddomain "github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func (s *Server) GetHouseholdProfiles(w http.ResponseWriter, r *http.Request, params GetHouseholdProfilesParams) {
	_ = params
	service := s.householdServiceSnapshot()
	if service == nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/unavailable", "Household Service Unavailable", problemcode.CodeUnavailable, "Household profile service is not initialized", nil)
		return
	}

	profiles, err := service.List(r.Context())
	if err != nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/read_failed", "Household Profile Read Failed", problemcode.CodeReadFailed, "Failed to load household profiles", nil)
		return
	}

	resp := make([]HouseholdProfile, 0, len(profiles))
	for _, profile := range profiles {
		resp = append(resp, householdProfileToAPI(profile))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) PostHouseholdProfiles(w http.ResponseWriter, r *http.Request, params PostHouseholdProfilesParams) {
	_ = params
	if _, ok := s.requireHouseholdSettingsAccess(w, r); !ok {
		return
	}

	service := s.householdServiceSnapshot()
	if service == nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/unavailable", "Household Service Unavailable", problemcode.CodeUnavailable, "Household profile service is not initialized", nil)
		return
	}

	var body HouseholdProfile
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "household/invalid_input", "Invalid Household Profile", problemcode.CodeInvalidInput, "The household profile body is malformed", nil)
		return
	}

	if _, err := service.Resolve(r.Context(), body.Id); err == nil {
		writeRegisteredProblem(w, r, http.StatusConflict, "household/conflict", "Household Profile Already Exists", problemcode.CodeConflict, "A household profile with this id already exists", nil)
		return
	} else if !errors.Is(err, householddomain.ErrNotFound) {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/read_failed", "Household Profile Read Failed", problemcode.CodeReadFailed, "Failed to check existing household profiles", nil)
		return
	}

	saved, err := service.Save(r.Context(), householdProfileFromAPI(body))
	if err != nil {
		writeHouseholdSaveError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(householdProfileToAPI(saved))
}

func (s *Server) PutHouseholdProfile(w http.ResponseWriter, r *http.Request, profileId string, params PutHouseholdProfileParams) {
	_ = params
	if _, ok := s.requireHouseholdSettingsAccess(w, r); !ok {
		return
	}

	service := s.householdServiceSnapshot()
	if service == nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/unavailable", "Household Service Unavailable", problemcode.CodeUnavailable, "Household profile service is not initialized", nil)
		return
	}

	var body HouseholdProfile
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "household/invalid_input", "Invalid Household Profile", problemcode.CodeInvalidInput, "The household profile body is malformed", nil)
		return
	}

	if strings.TrimSpace(profileId) == "" || !strings.EqualFold(strings.TrimSpace(profileId), strings.TrimSpace(body.Id)) {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "household/id_mismatch", "Household Profile Id Mismatch", problemcode.CodeInvalidInput, "Path profile id and body profile id must match", nil)
		return
	}

	if _, err := service.Resolve(r.Context(), profileId); err != nil {
		if errors.Is(err, householddomain.ErrNotFound) {
			writeRegisteredProblem(w, r, http.StatusNotFound, "household/not_found", "Household Profile Not Found", problemcode.CodeNotFound, "The requested household profile does not exist", nil)
			return
		}
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/read_failed", "Household Profile Read Failed", problemcode.CodeReadFailed, "Failed to load the household profile", nil)
		return
	}

	saved, err := service.Save(r.Context(), householdProfileFromAPI(body))
	if err != nil {
		writeHouseholdSaveError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(householdProfileToAPI(saved))
}

func (s *Server) DeleteHouseholdProfile(w http.ResponseWriter, r *http.Request, profileId string, params DeleteHouseholdProfileParams) {
	_ = params
	if _, ok := s.requireHouseholdSettingsAccess(w, r); !ok {
		return
	}

	service := s.householdServiceSnapshot()
	if service == nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/unavailable", "Household Service Unavailable", problemcode.CodeUnavailable, "Household profile service is not initialized", nil)
		return
	}

	if err := service.Delete(r.Context(), profileId); err != nil {
		switch {
		case errors.Is(err, householddomain.ErrNotFound):
			writeRegisteredProblem(w, r, http.StatusNotFound, "household/not_found", "Household Profile Not Found", problemcode.CodeNotFound, "The requested household profile does not exist", nil)
		case errors.Is(err, householddomain.ErrLastProfile):
			writeRegisteredProblem(w, r, http.StatusConflict, "household/delete_conflict", "Cannot Delete Last Household Profile", problemcode.CodeConflict, "At least one household profile must remain", nil)
		default:
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/delete_failed", "Household Profile Delete Failed", problemcode.CodeDeleteFailed, "Failed to delete the household profile", nil)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func householdProfileFromAPI(profile HouseholdProfile) householddomain.Profile {
	return householddomain.Profile{
		ID:                  profile.Id,
		Name:                profile.Name,
		Kind:                householddomain.ProfileKind(profile.Kind),
		MaxFSK:              profile.MaxFsk,
		AllowedBouquets:     append([]string(nil), profile.AllowedBouquets...),
		AllowedServiceRefs:  append([]string(nil), profile.AllowedServiceRefs...),
		FavoriteServiceRefs: append([]string(nil), profile.FavoriteServiceRefs...),
		Permissions: householddomain.Permissions{
			DVRPlayback: profile.Permissions.DvrPlayback,
			DVRManage:   profile.Permissions.DvrManage,
			Settings:    profile.Permissions.Settings,
		},
	}
}

func householdProfileToAPI(profile householddomain.Profile) HouseholdProfile {
	return HouseholdProfile{
		Id:                  profile.ID,
		Name:                profile.Name,
		Kind:                HouseholdProfileKind(profile.Kind),
		MaxFsk:              profile.MaxFSK,
		AllowedBouquets:     append([]string(nil), profile.AllowedBouquets...),
		AllowedServiceRefs:  append([]string(nil), profile.AllowedServiceRefs...),
		FavoriteServiceRefs: append([]string(nil), profile.FavoriteServiceRefs...),
		Permissions: HouseholdProfilePermissions{
			DvrPlayback: profile.Permissions.DVRPlayback,
			DvrManage:   profile.Permissions.DVRManage,
			Settings:    profile.Permissions.Settings,
		},
	}
}

func writeHouseholdSaveError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, householddomain.ErrInvalidProfileID):
		writeRegisteredProblem(w, r, http.StatusBadRequest, "household/invalid_input", "Invalid Household Profile", problemcode.CodeInvalidInput, "Household profile id must not be empty", nil)
	default:
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/save_failed", "Household Profile Save Failed", problemcode.CodeSaveFailed, "Failed to save the household profile", nil)
	}
}
