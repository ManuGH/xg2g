// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

const DedupLeaseHeldDetail = "dedup lease held"

func newReasonError(reason model.ReasonCode, detail string, err error) error {
	return lifecycle.NewReasonError(reason, detail, err)
}

func newReasonErrorWithDetail(reason model.ReasonCode, detailCode model.ReasonDetailCode, detailDebug string, err error) error {
	return lifecycle.NewReasonErrorWithDetail(reason, detailCode, detailDebug, err)
}

func wrapWithReasonClass(err error) error {
	return lifecycle.WrapWithReasonClass(err)
}
