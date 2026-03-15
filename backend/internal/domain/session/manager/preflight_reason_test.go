// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func TestPreflightStartReasonError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		preflightErr *ports.PreflightError
		wantReason   model.ReasonCode
		wantDetail   string
	}{
		{
			name:         "timeout maps to tune timeout",
			preflightErr: ports.NewPreflightError(ports.NewPreflightResult("timeout", 0, 0, 0, 17999)),
			wantReason:   model.RTuneTimeout,
			wantDetail:   "preflight failed timeout",
		},
		{
			name:         "invalid ts maps to upstream corrupt",
			preflightErr: ports.NewPreflightError(ports.NewPreflightResult("sync_miss", 0, 0, 0, 17999)),
			wantReason:   model.RUpstreamCorrupt,
			wantDetail:   "preflight failed invalid_ts: sync_miss",
		},
		{
			name:         "unauthorized maps to tune failed",
			preflightErr: ports.NewPreflightError(ports.NewPreflightResult("http_status_401", 401, 0, 0, 17999)),
			wantReason:   model.RTuneFailed,
			wantDetail:   "preflight failed unauthorized: http_status_401",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok, err := preflightStartReasonError(tc.preflightErr)
			if !ok {
				t.Fatal("expected structured preflight mapping")
			}
			reason, detailCode, detailDebug, ok := lifecycle.ReasonFromError(err)
			if !ok {
				t.Fatalf("expected lifecycle reason error, got %T", err)
			}
			if got := reason; got != tc.wantReason {
				t.Fatalf("ReasonFromError().reason = %q, want %q", got, tc.wantReason)
			}
			if detailCode != model.DNone && tc.wantReason != model.RTuneTimeout {
				t.Fatalf("unexpected detail code %q", detailCode)
			}
			if got := detailDebug; got != tc.wantDetail {
				t.Fatalf("detail debug = %q, want %q", got, tc.wantDetail)
			}
		})
	}
}
