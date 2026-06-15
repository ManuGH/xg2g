package v3

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/household"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// householdNoHeaderWarnInterval bounds how often the M24 no-header downgrade is logged at
// WARN. The downgrade happens on the request hot path; a header-less HLS client fetching a
// segment every few seconds would otherwise flood the log. We emit one WARN breadcrumb per
// interval (so the pattern stays discoverable) and DEBUG for the suppressed remainder.
const householdNoHeaderWarnInterval = 5 * time.Minute

// householdNoHeaderWarnAt holds the unix-nano timestamp of the last emitted WARN.
var householdNoHeaderWarnAt atomic.Int64

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

	// SECURITY (M24): absence of the profile header must never grant MORE privilege than
	// presence. When a household PIN is configured (parental controls are active) and no
	// profile is explicitly selected, resolve to the least-trusted restricted profile
	// instead of the unrestricted adult default — otherwise a restricted user escalates
	// simply by OMITTING the header. This holds regardless of unlock state (defense in
	// depth: a leaked/stale unlock must not turn a no-header request into the adult
	// identity, because that identity was never adult to begin with). Without a PIN there
	// are no parental controls configured, so the historical adult default is preserved.
	if !explicitHeader && pinConfigured {
		logHouseholdNoHeaderDowngrade(r)
		profile := household.CreateRestrictedProfile()
		return profile, household.AccessState{
			PinConfigured:  true,
			Unlocked:       unlocked,
			ExplicitHeader: false,
			Protected:      false,
		}, nil
	}

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

// householdNoHeaderShouldWarn reports whether the M24 no-header downgrade should be logged
// at WARN for the given timestamp, advancing the rate-limit window when it returns true.
// At most one caller per householdNoHeaderWarnInterval wins (CAS), the rest get DEBUG.
func householdNoHeaderShouldWarn(nowNano int64) bool {
	last := householdNoHeaderWarnAt.Load()
	return nowNano-last >= int64(householdNoHeaderWarnInterval) &&
		householdNoHeaderWarnAt.CompareAndSwap(last, nowNano)
}

// logHouseholdNoHeaderDowngrade emits a rate-limited breadcrumb for the M24 no-header
// downgrade. It runs on the request hot path, so it logs at WARN at most once per
// householdNoHeaderWarnInterval and at DEBUG otherwise — keeping the pattern discoverable
// for the "why is my content missing" case without flooding the log for a chatty
// header-less client (e.g. an HLS player fetching a segment every few seconds).
func logHouseholdNoHeaderDowngrade(r *http.Request) {
	warn := householdNoHeaderShouldWarn(time.Now().UnixNano())

	ev := log.L().Debug()
	if warn {
		ev = log.L().Warn()
	}
	ev.
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("resolved_profile", household.RestrictedProfileID).
		Bool("rate_limited", !warn).
		Msg("household: request without X-Household-Profile resolved to restricted profile (PIN configured)")
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
