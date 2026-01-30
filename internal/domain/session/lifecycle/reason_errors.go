// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type reasonError struct {
	reason model.ReasonCode
	detailCode  model.ReasonDetailCode
	detailDebug string
	err    error
}

func (e *reasonError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return string(e.reason)
}

func (e *reasonError) Is(target error) bool {
	if target == nil {
		return false
	}
	class := ReasonErrorClass(e.reason)
	return class != nil && target == class
}

func (e *reasonError) Unwrap() error {
	return e.err
}

func NewReasonError(reason model.ReasonCode, detail string, err error) error {
	return &reasonError{
		reason:      reason,
		detailCode:  model.DNone,
		detailDebug: detail,
		err:         err,
	}
}

func NewReasonErrorWithDetail(reason model.ReasonCode, detailCode model.ReasonDetailCode, detailDebug string, err error) error {
	return &reasonError{
		reason:      reason,
		detailCode:  detailCode,
		detailDebug: detailDebug,
		err:         err,
	}
}

func WrapWithReasonClass(err error) error {
	if err == nil {
		return nil
	}
	var rerr *reasonError
	if errors.As(err, &rerr) {
		return err
	}
	reason, detailCode, detailDebug := ClassifyReason(err)
	return NewReasonErrorWithDetail(reason, detailCode, detailDebug, err)
}

func ClassifyReason(err error) (model.ReasonCode, model.ReasonDetailCode, string) {
	if err == nil {
		return model.RNone, model.DNone, ""
	}

	if reason, detailCode, detailDebug, ok := ReasonFromError(err); ok {
		return reason, detailCode, sanitizeDetail(detailDebug)
	}

	if errors.Is(err, context.Canceled) {
		return model.RCancelled, model.DContextCanceled, ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.RTuneTimeout, model.DDeadlineExceeded, ""
	}

	// Legacy Enigma2 Error Handling (Decoupled via String Match)
	// Ideally the Adapter should wrap these.
	msg := err.Error()
	if strings.Contains(msg, "ready timeout") {
		return model.RTuneTimeout, model.DNone, "tuner ready timeout"
	}
	if strings.Contains(msg, "wrong service reference") {
		return model.RTuneFailed, model.DNone, "wrong service reference"
	}
	if strings.Contains(msg, "upstream unavailable") {
		return model.RTuneFailed, model.DNone, "upstream unavailable"
	}
	if strings.Contains(msg, "tuner not locked") {
		return model.RTuneFailed, model.DNone, "tuner not locked"
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return model.RProcessEnded, model.DNone, fmt.Sprintf("process exit code %d", exitErr.ExitCode())
	}

	return model.RUnknown, model.DNone, sanitizeDetail(err.Error())
}

func ReasonFromError(err error) (model.ReasonCode, model.ReasonDetailCode, string, bool) {
	var rerr *reasonError
	if errors.As(err, &rerr) {
		detailDebug := rerr.detailDebug
		if detailDebug == "" && rerr.err != nil {
			detailDebug = rerr.err.Error()
		}
		return rerr.reason, rerr.detailCode, detailDebug, true
	}
	return "", "", "", false
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
