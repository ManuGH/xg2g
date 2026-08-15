// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package devicebinding owns the single decision about whether a device's
// credentials are acceptable.
//
// It is deliberately a pure domain package: no HTTP, no store, no clock. The
// decision lives here rather than at each endpoint so there cannot be some
// endpoints answering 409, others 401, and others still quietly accepting
// unbound credentials. Transports map this decision; they do not make it.
//
// # Hard cutover
//
// There is no migration window, no cutoff date and no warning state. A
// credential is either bound to a device key or it is not, and an unbound one
// is refused. The Phase 0 census showed the affected fleet was stale — no
// active grants, last use months old — so a transition period would have
// protected nobody while adding a cutoff policy, reload monotonicity rules, a
// warning outcome, deadline headers and, later, the work of removing all of it
// again.
package devicebinding

// State is how a device's credentials are cryptographically bound.
type State string

const (
	// StateBound: bound to a device key, proven per request via DPoP.
	StateBound State = "bound"
	// StateLegacyUnbound: issued before binding existed. No longer accepted.
	StateLegacyUnbound State = "legacy_unbound"
)

// Outcome is what the caller must do.
type Outcome string

const (
	// OutcomeAllow: proceed.
	OutcomeAllow Outcome = "allow"

	// OutcomeDenyRepairRequired: refuse; the device must be paired again.
	//
	// Emphatically not an ordinary authentication failure. A client seeing 401
	// concludes "token stale, refresh and retry"; here no token issued to this
	// device will ever be accepted, because the device has no cryptographic
	// identity. It must map to a status a client will not feed back into
	// refresh logic.
	OutcomeDenyRepairRequired Outcome = "deny_repair_required"
)

// Decision is the result of the central evaluation.
type Decision struct {
	Outcome Outcome
	State   State
}

// RequiresRePairing reports whether the user must pair this device again.
func (d Decision) RequiresRePairing() bool { return d.Outcome == OutcomeDenyRepairRequired }

// Denied reports whether the request must be refused.
func (d Decision) Denied() bool { return d.Outcome == OutcomeDenyRepairRequired }

// Evaluate makes the binding decision. This is the only place it is made.
//
// Anything not explicitly bound is refused, including an unrecognised value: an
// unknown state must never be promoted to bound, because assuming security that
// was never established is the one direction this must not fail in.
func Evaluate(state State) Decision {
	if state == StateBound {
		return Decision{Outcome: OutcomeAllow, State: StateBound}
	}
	return Decision{Outcome: OutcomeDenyRepairRequired, State: StateLegacyUnbound}
}
