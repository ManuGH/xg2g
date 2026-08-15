// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package auth

import "github.com/ManuGH/xg2g/internal/domain/devicebinding"

// Outcome is *why* an authentication attempt ended the way it did.
//
// It replaces the `(*Principal, bool)` shape, which could only say yes or no.
// Everything that was not a plain success — an absent token, an invalid one, a
// token whose configuration grants no scopes, a device in an expiring security
// model — collapsed into the same `false` and surfaced as an anonymous 401.
// That is indistinguishable from an ordinary login defect, both for a client
// deciding whether to retry and for anyone reading logs.
//
// This type carries **no transport concepts**. It says what happened; the HTTP
// layer decides what status that becomes, so the same decision remains usable
// from any other front end.
type Outcome string

//nolint:gosec // G101: Outcome names are status identifiers, not hardcoded credentials
const (
	// OutcomeAuthenticated: proceed normally.
	OutcomeAuthenticated Outcome = "authenticated"

	// OutcomeNoCredentials: nothing was presented.
	OutcomeNoCredentials Outcome = "no_credentials"

	// OutcomeInvalidCredentials: something was presented and did not verify.
	OutcomeInvalidCredentials Outcome = "invalid_credentials"

	// OutcomeMisconfiguredToken: the token verified, but its configuration
	// grants no scopes. A deployment error rather than a client error, and
	// previously indistinguishable from a bad token.
	OutcomeMisconfiguredToken Outcome = "misconfigured_token"

	// OutcomeDeviceRepairRequired: refused because the device must be paired
	// again. Deliberately distinct from invalid credentials: refreshing cannot
	// fix it, so a client must not treat it as a refresh case.
	OutcomeDeviceRepairRequired Outcome = "device_repair_required"
)

// Result is the outcome of one authentication attempt.
type Result struct {
	Principal *Principal
	Outcome   Outcome
	// Binding is set whenever a device-binding decision took part, so the
	// transport can report the deadline without re-deriving it.
	Binding *devicebinding.Decision
}

// OK reports whether the request may proceed.
func (r Result) OK() bool {
	return r.Principal != nil && r.Outcome != OutcomeDeviceRepairRequired
}

// RequiresRePairing reports whether the user must pair this device again.
func (r Result) RequiresRePairing() bool {
	return r.Outcome == OutcomeDeviceRepairRequired
}

func Authenticated(principal *Principal) Result {
	if principal == nil {
		return InvalidCredentials()
	}
	return Result{Principal: principal, Outcome: OutcomeAuthenticated}
}

// AuthenticatedDevice is a success where a device-binding decision took part.
//
// The decision is carried even when it allowed the request, so the transport can
// report the device's state without recomputing the policy. A `nil` Binding
// therefore means "this was not a paired-device session" — never "unbound".
func AuthenticatedDevice(principal *Principal, decision devicebinding.Decision) Result {
	if principal == nil {
		return InvalidCredentials()
	}
	return Result{Principal: principal, Outcome: OutcomeAuthenticated, Binding: &decision}
}

func RepairRequired(decision devicebinding.Decision) Result {
	return Result{Outcome: OutcomeDeviceRepairRequired, Binding: &decision}
}

func NoCredentials() Result { return Result{Outcome: OutcomeNoCredentials} }

func InvalidCredentials() Result { return Result{Outcome: OutcomeInvalidCredentials} }

func MisconfiguredToken() Result { return Result{Outcome: OutcomeMisconfiguredToken} }
