// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package auth

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/devicebinding"
)

func principal() *Principal {
	return NewPrincipal("token", "alice", []string{"v3:read"})
}

func TestAuthenticatedProceeds(t *testing.T) {
	result := Authenticated(principal())

	if !result.OK() {
		t.Error("an authenticated result must proceed")
	}
	if result.RequiresRePairing() {
		t.Error("an ordinary success must not ask for re-pairing")
	}
	if result.Binding != nil {
		t.Error("no binding decision took part")
	}
}

func TestRepairRequiredIsRefused(t *testing.T) {
	decision := devicebinding.Decision{
		Outcome: devicebinding.OutcomeDenyRepairRequired,
		State:   devicebinding.StateLegacyUnbound,
	}

	result := RepairRequired(decision)

	if result.OK() {
		t.Error("a repair-required result must not proceed")
	}
	if !result.RequiresRePairing() {
		t.Error("denial must still state the required user action")
	}
	if result.Principal != nil {
		t.Error("a refused request must not carry a principal")
	}
}

// The reasons that used to be one indistinguishable `false`.
func TestFailureReasonsAreDistinguishable(t *testing.T) {
	cases := map[string]struct {
		result Result
		want   Outcome
	}{
		"nothing presented":   {NoCredentials(), OutcomeNoCredentials},
		"did not verify":      {InvalidCredentials(), OutcomeInvalidCredentials},
		"granted no scopes":   {MisconfiguredToken(), OutcomeMisconfiguredToken},
		"device must re-pair": {RepairRequired(devicebinding.Decision{}), OutcomeDeviceRepairRequired},
	}

	seen := map[Outcome]string{}
	for name, tc := range cases {
		if tc.result.Outcome != tc.want {
			t.Errorf("%s: outcome = %s, want %s", name, tc.result.Outcome, tc.want)
		}
		if tc.result.OK() {
			t.Errorf("%s: must not proceed", name)
		}
		if previous, clash := seen[tc.result.Outcome]; clash {
			t.Errorf("%s and %s share outcome %s", name, previous, tc.result.Outcome)
		}
		seen[tc.result.Outcome] = name
	}
}

// A constructor handed a nil principal must not manufacture a success.
func TestNilPrincipalCannotAuthenticate(t *testing.T) {
	if Authenticated(nil).OK() {
		t.Error("Authenticated(nil) must not proceed")
	}
	if AuthenticatedDevice(nil, devicebinding.Decision{}).OK() {
		t.Error("AuthenticatedDevice(nil, …) must not proceed")
	}
}
