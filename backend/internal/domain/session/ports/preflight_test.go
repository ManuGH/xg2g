// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import (
	"errors"
	"net/http"
	"testing"
)

func TestClassifyPreflightReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		detail     string
		httpStatus int
		want       PreflightReason
	}{
		{name: "empty defaults to invalid ts", want: PreflightReasonInvalidTS},
		{name: "sync miss", detail: "sync_miss", want: PreflightReasonInvalidTS},
		{name: "short read", detail: "short_read", want: PreflightReasonCorruptInput},
		{name: "timeout detail", detail: "timeout", want: PreflightReasonTimeout},
		{name: "request failed", detail: "request_failed", want: PreflightReasonUnreachable},
		{name: "http status detail unauthorized", detail: "http_status_401", want: PreflightReasonUnauthorized},
		{name: "http status detail forbidden", detail: "http_status_403", want: PreflightReasonForbidden},
		{name: "http status arg bad gateway", httpStatus: http.StatusServiceUnavailable, want: PreflightReasonBadGateway},
		{name: "no video", detail: "no_video", want: PreflightReasonNoVideo},
		{name: "fallback invalid", detail: "fallback_url_invalid", want: PreflightReasonFallbackURLInvalid},
		{name: "fallback failed", detail: "fallback_failed_all", want: PreflightReasonFallbackFailed},
		{name: "invalid source", detail: "invalid_url", want: PreflightReasonInvalidSource},
		{name: "unknown stays unknown", detail: "something_new", want: PreflightReasonUnknown},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyPreflightReason(tc.detail, tc.httpStatus)
			if got != tc.want {
				t.Fatalf("ClassifyPreflightReason(%q, %d) = %q, want %q", tc.detail, tc.httpStatus, got, tc.want)
			}
		})
	}
}

func TestPreflightResultNormalized(t *testing.T) {
	t.Parallel()

	result := NewPreflightResult("sync_miss", 0, 188, 12, 17999)
	if result.OK {
		t.Fatal("expected failed preflight result")
	}
	if result.Reason != PreflightReasonInvalidTS {
		t.Fatalf("expected invalid_ts, got %q", result.Reason)
	}
	if result.Detail != "sync_miss" {
		t.Fatalf("expected sync_miss detail, got %q", result.Detail)
	}
	if result.FailureDetail() != "sync_miss" {
		t.Fatalf("expected sync_miss failure detail, got %q", result.FailureDetail())
	}
}

func TestPreflightErrorStructuredResultLegacyReason(t *testing.T) {
	t.Parallel()

	err := &PreflightError{Reason: "sync_miss"}
	result := err.StructuredResult()

	if result.OK {
		t.Fatal("expected failed structured result")
	}
	if result.Reason != PreflightReasonInvalidTS {
		t.Fatalf("expected invalid_ts, got %q", result.Reason)
	}
	if result.Detail != "sync_miss" {
		t.Fatalf("expected sync_miss detail, got %q", result.Detail)
	}
	if !errors.Is(err, ErrNoValidTS) {
		t.Fatalf("expected ErrNoValidTS unwrap, got %v", err)
	}
	if got := err.Error(); got != "preflight no valid ts: sync_miss" {
		t.Fatalf("unexpected error string %q", got)
	}
}

func TestNewPreflightErrorKeepsLegacySurface(t *testing.T) {
	t.Parallel()

	err := NewPreflightError(PreflightResult{
		Reason:     PreflightReasonTimeout,
		HTTPStatus: http.StatusGatewayTimeout,
		LatencyMs:  2500,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNoValidTS) {
		t.Fatalf("expected ErrNoValidTS unwrap, got %v", err)
	}
	result := err.StructuredResult()
	if result.Reason != PreflightReasonTimeout {
		t.Fatalf("expected timeout result, got %q", result.Reason)
	}
	if got := err.Error(); got != "preflight no valid ts: timeout" {
		t.Fatalf("unexpected error string %q", got)
	}
}
