// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

const DedupLeaseHeldDetail = "dedup lease held"

type reasonError struct {
	reason model.ReasonCode
	detail string
	err    error
}

func (e *reasonError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return string(e.reason)
}

func (e *reasonError) Unwrap() error {
	return e.err
}

func newReasonError(reason model.ReasonCode, detail string, err error) error {
	return &reasonError{
		reason: reason,
		detail: detail,
		err:    err,
	}
}

func classifyReason(err error) (model.ReasonCode, string) {
	if err == nil {
		return model.RNone, ""
	}

	if reason, detail, ok := reasonFromError(err); ok {
		return reason, sanitizeDetail(detail)
	}

	if errors.Is(err, context.Canceled) {
		return model.RClientStop, "context canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.RTuneTimeout, "deadline exceeded"
	}
	if errors.Is(err, enigma2.ErrReadyTimeout) {
		return model.RTuneTimeout, "tuner ready timeout"
	}
	if errors.Is(err, enigma2.ErrWrongServiceRef) {
		return model.RTuneFailed, "wrong service reference"
	}
	if errors.Is(err, enigma2.ErrUpstreamUnavailable) {
		return model.RTuneFailed, "upstream unavailable"
	}
	if errors.Is(err, enigma2.ErrNotLocked) {
		return model.RTuneFailed, "tuner not locked"
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return model.RProcessEnded, fmt.Sprintf("process exit code %d", exitErr.ExitCode())
	}

	return model.RUnknown, sanitizeDetail(err.Error())
}

func reasonFromError(err error) (model.ReasonCode, string, bool) {
	var rerr *reasonError
	if errors.As(err, &rerr) {
		detail := rerr.detail
		if detail == "" && rerr.err != nil {
			detail = rerr.err.Error()
		}
		return rerr.reason, detail, true
	}
	return "", "", false
}

func sanitizeDetail(detail string) string {
	if detail == "" {
		return ""
	}
	const maxLen = 160
	clean := strings.ReplaceAll(detail, "\n", " ")
	if len(clean) > maxLen {
		return clean[:maxLen] + "..."
	}
	return clean
}
