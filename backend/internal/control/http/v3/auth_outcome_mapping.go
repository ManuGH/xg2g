// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// authRejection is the transport mapping of a failed authentication.
//
// This is the *only* place an `auth.Outcome` becomes HTTP. The domain decides
// what happened; this decides what that looks like on the wire. Keeping the two
// apart is what lets the same decision serve another front end later — and what
// stops every non-success collapsing back into an anonymous 401 at the last
// step, which would discard exactly the information the typed outcome exists to
// carry.
type authRejection struct {
	status      int
	problemType string
	title       string
	code        string
	detail      string
}

func rejectionFor(outcome auth.Outcome) authRejection {
	switch outcome {
	case auth.OutcomeDeviceRepairRequired:
		// Not 401. A client reads 401 as "refresh and retry", and refreshing
		// can never fix this: the device is in a retired security model and
		// every token issued to it will be refused. 409 says the device's
		// current state conflicts with the requirement, which is what is
		// actually true.
		return authRejection{
			status:      http.StatusConflict,
			problemType: "auth/device_reauth_required",
			title:       "Device must be paired again",
			code:        problemcode.CodeDeviceReauthRequired,
			detail:      "This device's credentials predate device binding. Pair the device again; refreshing the token cannot restore access.",
		}

	case auth.OutcomeMisconfiguredToken:
		// A server-side fault, not a client one. Behind a 401 it reads as
		// "your credentials are broken" and sends the investigation to the
		// wrong side entirely: the caller cannot fix a token that the server
		// configuration grants no scopes.
		return authRejection{
			status:      http.StatusServiceUnavailable,
			problemType: "auth/server_token_misconfigured",
			title:       "Server token configuration is incomplete",
			code:        problemcode.CodeServerTokenMisconfigured,
			detail:      "The presented token is recognised but the server grants it no scopes. This is a server configuration fault; retrying with different credentials will not help.",
		}

	default:
		return authRejection{
			status:      http.StatusUnauthorized,
			problemType: "auth/unauthorized",
			title:       "Unauthorized",
			code:        problemcode.CodeUnauthorized,
			detail:      "Authentication required",
		}
	}
}

// writeAuthRejection renders the mapped rejection as an RFC 7807 document.
func writeAuthRejection(w http.ResponseWriter, r *http.Request, result auth.Result) {
	rejection := rejectionFor(result.Outcome)

	writeRegisteredProblem(w, r, rejection.status, rejection.problemType, rejection.title, rejection.code, rejection.detail, nil)
}
