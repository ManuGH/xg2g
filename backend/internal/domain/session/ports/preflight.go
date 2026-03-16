// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import (
	"net/http"
	"strconv"
	"strings"
)

// PreflightReason is the bounded taxonomy for structured media preflight failures.
type PreflightReason string

const (
	PreflightReasonUnknown            PreflightReason = "unknown"
	PreflightReasonTimeout            PreflightReason = "timeout"
	PreflightReasonUnreachable        PreflightReason = "unreachable"
	PreflightReasonUnauthorized       PreflightReason = "unauthorized"
	PreflightReasonForbidden          PreflightReason = "forbidden"
	PreflightReasonNotFound           PreflightReason = "not_found"
	PreflightReasonBadGateway         PreflightReason = "bad_gateway"
	PreflightReasonInvalidTS          PreflightReason = "invalid_ts"
	PreflightReasonNoVideo            PreflightReason = "no_video"
	PreflightReasonCorruptInput       PreflightReason = "corrupt_input"
	PreflightReasonInvalidSource      PreflightReason = "invalid_source"
	PreflightReasonFallbackURLInvalid PreflightReason = "fallback_url_invalid"
	PreflightReasonFallbackFailed     PreflightReason = "fallback_failed"
	PreflightReasonInternal           PreflightReason = "internal"
)

// PreflightResult captures the structured outcome of a media-path preflight.
type PreflightResult struct {
	OK           bool
	Reason       PreflightReason
	Detail       string
	HTTPStatus   int
	Bytes        int
	LatencyMs    int64
	ResolvedPort int
}

// FailureDetail returns the most specific human-readable failure detail.
func (r PreflightResult) FailureDetail() string {
	if detail := strings.TrimSpace(r.Detail); detail != "" {
		return detail
	}
	if r.Reason != "" && r.Reason != PreflightReasonUnknown {
		return string(r.Reason)
	}
	return ""
}

// Normalized returns the result with a bounded failure reason inferred from detail/status.
func (r PreflightResult) Normalized() PreflightResult {
	out := r
	out.Detail = strings.TrimSpace(out.Detail)
	if out.OK {
		out.Reason = ""
		return out
	}
	if out.Reason == "" {
		out.Reason = ClassifyPreflightReason(out.Detail, out.HTTPStatus)
	}
	if out.Reason == "" || out.Reason == PreflightReasonUnknown {
		if out.Detail == "" && out.HTTPStatus == 0 {
			out.Reason = PreflightReasonInvalidTS
		}
	}
	return out
}

// NewPreflightResult constructs a normalized failed preflight result from legacy detail/status fields.
func NewPreflightResult(detail string, httpStatus, bytes int, latencyMs int64, resolvedPort int) PreflightResult {
	return PreflightResult{
		Detail:       strings.TrimSpace(detail),
		HTTPStatus:   httpStatus,
		Bytes:        bytes,
		LatencyMs:    latencyMs,
		ResolvedPort: resolvedPort,
	}.Normalized()
}

// NewSuccessfulPreflightResult constructs a normalized successful preflight result.
func NewSuccessfulPreflightResult(bytes int, latencyMs int64, resolvedPort int) PreflightResult {
	return PreflightResult{
		OK:           true,
		Bytes:        bytes,
		LatencyMs:    latencyMs,
		ResolvedPort: resolvedPort,
	}.Normalized()
}

// ClassifyPreflightReason maps legacy detail/status signals into the bounded preflight taxonomy.
func ClassifyPreflightReason(detail string, httpStatus int) PreflightReason {
	if reason := classifyPreflightHTTPStatus(httpStatus); reason != "" {
		return reason
	}

	raw := strings.TrimSpace(strings.ToLower(detail))
	switch raw {
	case "", "no_valid_ts", "invalid_ts":
		return PreflightReasonInvalidTS
	case "sync_miss":
		return PreflightReasonInvalidTS
	case "short_read":
		return PreflightReasonCorruptInput
	case "timeout":
		return PreflightReasonTimeout
	case "request_failed", "unreachable":
		return PreflightReasonUnreachable
	case "unauthorized":
		return PreflightReasonUnauthorized
	case "forbidden":
		return PreflightReasonForbidden
	case "not_found":
		return PreflightReasonNotFound
	case "bad_gateway":
		return PreflightReasonBadGateway
	case "no_video":
		return PreflightReasonNoVideo
	case "corrupt_input":
		return PreflightReasonCorruptInput
	case "empty_url", "invalid_url":
		return PreflightReasonInvalidSource
	case "request_build_failed":
		return PreflightReasonInternal
	case "fallback_url_invalid":
		return PreflightReasonFallbackURLInvalid
	case "fallback_failed_all", "fallback_failed":
		return PreflightReasonFallbackFailed
	case "internal":
		return PreflightReasonInternal
	}

	if strings.HasPrefix(raw, "http_status_") {
		code, err := strconv.Atoi(strings.TrimPrefix(raw, "http_status_"))
		if err == nil {
			if reason := classifyPreflightHTTPStatus(code); reason != "" {
				return reason
			}
		}
	}

	switch {
	case strings.Contains(raw, "timeout"):
		return PreflightReasonTimeout
	case strings.Contains(raw, "unauthorized"):
		return PreflightReasonUnauthorized
	case strings.Contains(raw, "forbidden"):
		return PreflightReasonForbidden
	case strings.Contains(raw, "not_found"):
		return PreflightReasonNotFound
	case strings.Contains(raw, "bad_gateway"):
		return PreflightReasonBadGateway
	case strings.Contains(raw, "no_video"):
		return PreflightReasonNoVideo
	case strings.Contains(raw, "corrupt"), strings.Contains(raw, "short_read"):
		return PreflightReasonCorruptInput
	case strings.Contains(raw, "sync"), strings.Contains(raw, "ts"):
		return PreflightReasonInvalidTS
	case strings.Contains(raw, "request_failed"), strings.Contains(raw, "unreachable"):
		return PreflightReasonUnreachable
	case strings.Contains(raw, "invalid_url"), strings.Contains(raw, "empty_url"):
		return PreflightReasonInvalidSource
	case strings.Contains(raw, "fallback_url_invalid"):
		return PreflightReasonFallbackURLInvalid
	case strings.Contains(raw, "fallback_failed"):
		return PreflightReasonFallbackFailed
	}

	return PreflightReasonUnknown
}

func classifyPreflightHTTPStatus(status int) PreflightReason {
	switch status {
	case http.StatusUnauthorized:
		return PreflightReasonUnauthorized
	case http.StatusForbidden:
		return PreflightReasonForbidden
	case http.StatusNotFound:
		return PreflightReasonNotFound
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return PreflightReasonBadGateway
	case http.StatusGatewayTimeout:
		return PreflightReasonTimeout
	}
	return ""
}
