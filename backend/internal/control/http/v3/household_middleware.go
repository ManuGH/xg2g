package v3

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func (s *Server) householdMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		profile, accessState, err := s.resolveHouseholdProfile(r)
		if err != nil {
			if errors.Is(err, household.ErrNotFound) {
				writeRegisteredProblem(w, r, http.StatusBadRequest, "household/invalid_profile", "Invalid Household Profile", problemcode.CodeInvalidInput, "The supplied household profile is unknown", map[string]any{
					"header": household.ProfileHeader,
				})
				return
			}
			writeRegisteredProblem(w, r, http.StatusInternalServerError, "household/profile_resolution_failed", "Household Profile Resolution Failed", problemcode.CodeInternalError, "Failed to resolve household profile", nil)
			return
		}

		ctx := household.WithProfile(r.Context(), &profile)
		ctx = household.WithAccessState(ctx, accessState)
		if accessState.Protected && !s.householdUnlockBypass(r) {
			writeRegisteredProblem(w, r, http.StatusForbidden, "household/pin_required", "Household Pin Required", problemcode.CodeForbidden, "Switching to this household profile requires the configured household pin", map[string]any{
				"profileId": profile.ID,
			})
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) resolveHouseholdProfile(r *http.Request) (household.Profile, household.AccessState, error) {
	service := s.householdServiceSnapshot()
	headerValue := strings.TrimSpace(r.Header.Get(household.ProfileHeader))
	explicitHeader := headerValue != ""
	unlocked := s.isHouseholdUnlocked(r)
	pinConfigured := s.GetConfig().Household.PinConfigured()
	if service == nil {
		profile := household.CreateDefaultProfile()
		return profile, household.AccessState{
			PinConfigured:  pinConfigured,
			Unlocked:       unlocked,
			ExplicitHeader: explicitHeader,
			Protected:      pinConfigured && explicitHeader && profile.Kind == household.ProfileKindAdult && !unlocked,
		}, nil
	}

	profile, err := service.Resolve(r.Context(), headerValue)
	if err != nil {
		return household.Profile{}, household.AccessState{}, err
	}

	return profile, household.AccessState{
		PinConfigured:  pinConfigured,
		Unlocked:       unlocked,
		ExplicitHeader: explicitHeader,
		Protected:      pinConfigured && explicitHeader && profile.Kind == household.ProfileKindAdult && !unlocked,
	}, nil
}

func (s *Server) householdUnlockBypass(r *http.Request) bool {
	if r == nil {
		return false
	}

	path := strings.TrimSpace(r.URL.Path)
	switch {
	case strings.HasSuffix(path, "/auth/session"):
		return r.Method == http.MethodDelete
	case strings.HasSuffix(path, "/household/unlock"):
		return true
	case strings.HasSuffix(path, "/household/profiles"):
		return r.Method == http.MethodGet
	default:
		return false
	}
}
