// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func preflightStartReasonError(err error) (error, *ports.PreflightError, bool) {
	var pErr *ports.PreflightError
	if !errors.As(err, &pErr) {
		return nil, nil, false
	}

	result := pErr.StructuredResult()
	detail := preflightFailureDetail(result)

	switch result.Reason {
	case ports.PreflightReasonTimeout:
		return newReasonErrorWithDetail(model.RTuneTimeout, model.DDeadlineExceeded, detail, err), pErr, true
	case ports.PreflightReasonInvalidTS, ports.PreflightReasonNoVideo, ports.PreflightReasonCorruptInput:
		return newReasonError(model.RUpstreamCorrupt, detail, err), pErr, true
	default:
		return newReasonError(model.RTuneFailed, detail, err), pErr, true
	}
}

func preflightFailureDetail(result ports.PreflightResult) string {
	reason := strings.TrimSpace(string(result.Reason))
	detail := strings.TrimSpace(result.FailureDetail())

	switch {
	case reason != "" && detail != "" && detail != reason:
		return fmt.Sprintf("preflight failed %s: %s", reason, detail)
	case reason != "":
		return "preflight failed " + reason
	case detail != "":
		return "preflight failed " + detail
	default:
		return "preflight failed"
	}
}
