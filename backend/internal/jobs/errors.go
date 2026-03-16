// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/url"
	"strings"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/rs/zerolog"
)

// OpError carries a stable code for background-job observability and retry policy.
type OpError struct {
	Code      string
	Retryable bool
	Err       error
}

func (e *OpError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *OpError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrapJobError(code string, retryable bool, err error) error {
	if err == nil {
		return nil
	}
	var existing *OpError
	if errors.As(err, &existing) {
		return err
	}
	return &OpError{Code: code, Retryable: retryable, Err: err}
}

func WrapRefreshConfigError(err error) error {
	return wrapJobError(problemcode.CodeJobConfigInvalid, false, err)
}

func WrapBouquetsFetchError(err error) error {
	return wrapExternalJobError(problemcode.CodeJobBouquetsFetchFailed, err)
}

func WrapBouquetNotFoundError(err error) error {
	return wrapJobError(problemcode.CodeJobBouquetNotFound, false, err)
}

func WrapServicesFetchError(err error) error {
	return wrapExternalJobError(problemcode.CodeJobServicesFetchFailed, err)
}

func WrapStreamURLBuildError(err error) error {
	return wrapExternalJobError(problemcode.CodeJobStreamURLBuildFailed, err)
}

func WrapPlaylistPathError(err error) error {
	return wrapJobError(problemcode.CodeJobPlaylistPathInvalid, false, err)
}

func WrapPlaylistWriteError(err error) error {
	if isPermissionError(err) {
		return wrapJobError(problemcode.CodeJobPlaylistWritePerm, false, err)
	}
	return wrapJobError(problemcode.CodeJobPlaylistWriteFailed, false, err)
}

func WrapXMLTVWriteError(err error) error {
	if isPermissionError(err) {
		return wrapJobError(problemcode.CodeJobXMLTVWritePerm, false, err)
	}
	return wrapJobError(problemcode.CodeJobXMLTVWriteFailed, false, err)
}

func WrapEPGFetchError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "invalid empty sRef") {
		return wrapJobError(problemcode.CodeJobEPGFetchInvalidInput, false, err)
	}

	code, retryable := classifyExternalJobError(problemcode.CodeJobEPGFetchFailed, err)
	if code == problemcode.CodeJobEPGFetchFailed && retryable {
		code = problemcode.CodeJobEPGFetchUnavailable
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeoutError(err) {
		code = problemcode.CodeJobEPGFetchTimeout
		retryable = true
	}
	return wrapJobError(code, retryable, err)
}

func JobErrorCode(err error) string {
	var opErr *OpError
	if errors.As(err, &opErr) && strings.TrimSpace(opErr.Code) != "" {
		return opErr.Code
	}
	return problemcode.CodeInternalError
}

func JobErrorRetryable(err error) bool {
	var opErr *OpError
	return errors.As(err, &opErr) && opErr.Retryable
}

func logJobError(job string, event *zerolog.Event, err error) *zerolog.Event {
	code := JobErrorCode(err)
	retryable := JobErrorRetryable(err)
	metrics.IncJobError(job, code, retryable)
	return event.Str("code", code).Bool("retryable", retryable)
}

func wrapExternalJobError(defaultCode string, err error) error {
	code, retryable := classifyExternalJobError(defaultCode, err)
	return wrapJobError(code, retryable, err)
}

func classifyExternalJobError(defaultCode string, err error) (string, bool) {
	switch {
	case err == nil:
		return defaultCode, false
	case errors.Is(err, context.DeadlineExceeded):
		return defaultCode, true
	case errors.Is(err, context.Canceled):
		return defaultCode, false
	case isTimeoutError(err):
		return defaultCode, true
	case isUnavailableError(err):
		return defaultCode, true
	default:
		return defaultCode, false
	}
}

func isPermissionError(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isUnavailableError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}
