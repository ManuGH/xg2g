// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// A client seeing 401 refreshes and retries. Refreshing can never fix a retired
// security model, so this must not be a 401.
func TestRepairRequiredIsNotUnauthorized(t *testing.T) {
	rejection := rejectionFor(auth.OutcomeDeviceRepairRequired)

	if rejection.status == http.StatusUnauthorized {
		t.Fatal("device_repair_required must not map to 401; a client would feed it into refresh logic")
	}
	if rejection.status != http.StatusConflict {
		t.Errorf("status = %d, want %d", rejection.status, http.StatusConflict)
	}
	if rejection.code != problemcode.CodeDeviceReauthRequired {
		t.Errorf("code = %s, want %s", rejection.code, problemcode.CodeDeviceReauthRequired)
	}
}

// A deployment fault behind a 401 sends the investigation to the client, which
// cannot fix a token the server grants no scopes.
func TestMisconfiguredTokenIsReportedAsAServerFault(t *testing.T) {
	rejection := rejectionFor(auth.OutcomeMisconfiguredToken)

	if rejection.status == http.StatusUnauthorized {
		t.Fatal("misconfigured_token must not map to 401; it is a server-side fault")
	}
	if rejection.status < 500 {
		t.Errorf("status = %d, want a 5xx", rejection.status)
	}
	if rejection.code != problemcode.CodeServerTokenMisconfigured {
		t.Errorf("code = %s, want %s", rejection.code, problemcode.CodeServerTokenMisconfigured)
	}
}

func TestOrdinaryFailuresRemainUnauthorized(t *testing.T) {
	for _, outcome := range []auth.Outcome{auth.OutcomeNoCredentials, auth.OutcomeInvalidCredentials} {
		rejection := rejectionFor(outcome)
		if rejection.status != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", outcome, rejection.status)
		}
		if rejection.code != problemcode.CodeUnauthorized {
			t.Errorf("%s: code = %s, want %s", outcome, rejection.code, problemcode.CodeUnauthorized)
		}
	}
}

// An outcome nobody mapped must not accidentally become a success or a 5xx.
func TestUnknownOutcomeFallsBackToUnauthorized(t *testing.T) {
	rejection := rejectionFor(auth.Outcome("something-new"))

	if rejection.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an unmapped outcome", rejection.status)
	}
}

// The three mapped rejections must stay distinguishable on the wire, or the
// typed outcomes buy nothing at the HTTP boundary.
func TestMappedRejectionsAreDistinctOnTheWire(t *testing.T) {
	outcomes := []auth.Outcome{
		auth.OutcomeInvalidCredentials,
		auth.OutcomeMisconfiguredToken,
		auth.OutcomeDeviceRepairRequired,
	}

	seenStatus := map[int]auth.Outcome{}
	seenCode := map[string]auth.Outcome{}

	for _, outcome := range outcomes {
		rejection := rejectionFor(outcome)
		if previous, clash := seenStatus[rejection.status]; clash {
			t.Errorf("%s and %s share status %d", outcome, previous, rejection.status)
		}
		if previous, clash := seenCode[rejection.code]; clash {
			t.Errorf("%s and %s share code %s", outcome, previous, rejection.code)
		}
		seenStatus[rejection.status] = outcome
		seenCode[rejection.code] = outcome
	}
}

// Every mapped code must exist in the registry, or writeRegisteredProblem
// panics at runtime through MustResolve.
func TestMappedCodesAreRegistered(t *testing.T) {
	for _, outcome := range []auth.Outcome{
		auth.OutcomeNoCredentials,
		auth.OutcomeInvalidCredentials,
		auth.OutcomeMisconfiguredToken,
		auth.OutcomeDeviceRepairRequired,
	} {
		code := rejectionFor(outcome).code
		if _, ok := problemcode.Lookup(code); !ok {
			t.Errorf("%s maps to unregistered code %s", outcome, code)
		}
	}
}
