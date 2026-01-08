// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

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

	// Legacy Enigma2 Error Handling (Decoupled via String Match)
	// Ideally the Adapter should wrap these.
	msg := err.Error()
	if strings.Contains(msg, "ready timeout") {
		return model.RTuneTimeout, "tuner ready timeout"
	}
	if strings.Contains(msg, "wrong service reference") {
		return model.RTuneFailed, "wrong service reference"
	}
	if strings.Contains(msg, "upstream unavailable") {
		return model.RTuneFailed, "upstream unavailable"
	}
	if strings.Contains(msg, "tuner not locked") {
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
