// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import "github.com/ManuGH/xg2g/internal/problemcode"

func newRegisteredAPIError(code, title string) *APIError {
	spec := problemcode.MustResolve(code, title)
	return &APIError{
		Code:    spec.Code,
		Message: spec.Title,
	}
}

func problemSpecForCode(code, title, detail string) terminalProblemSpec {
	spec := problemcode.Resolve(code, title)
	return terminalProblemSpec{
		problemType: spec.ProblemType,
		title:       spec.Title,
		code:        spec.Code,
		detail:      detail,
	}
}

func problemSpecForAPIError(apiErr *APIError, detail string) terminalProblemSpec {
	if apiErr == nil {
		return terminalProblemSpec{detail: detail}
	}
	return problemSpecForCode(apiErr.Code, apiErr.Message, detail)
}
