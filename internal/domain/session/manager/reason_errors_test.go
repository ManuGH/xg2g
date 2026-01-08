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
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/require"
)

func TestClassifyReason(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		wantReason     model.ReasonCode
		wantDetail     string
		detailContains bool
	}{
		{
			name:       "lease busy explicit",
			err:        newReasonError(model.RLeaseBusy, "no tuner slots available", nil),
			wantReason: model.RLeaseBusy,
			wantDetail: "no tuner slots available",
		},
		{
			name:       "detail sanitized",
			err:        newReasonError(model.RLeaseBusy, "line1\nline2", nil),
			wantReason: model.RLeaseBusy,
			wantDetail: "line1 line2",
		},
		{
			name:       "tune timeout",
			err:        fmt.Errorf("tuner readiness failed: %w", errors.New("tuner ready timeout")),
			wantReason: model.RTuneTimeout,
			wantDetail: "tuner ready timeout",
		},
		{
			name:       "tune failed upstream",
			err:        fmt.Errorf("zap failed: %w", errors.New("upstream unavailable")),
			wantReason: model.RTuneFailed,
			wantDetail: "upstream unavailable",
		},
		{
			name:       "ffmpeg start failed",
			err:        newReasonError(model.RPipelineStartFailed, "transcoder init failed", errors.New("boom")),
			wantReason: model.RPipelineStartFailed,
			wantDetail: "transcoder init failed",
		},
		{
			name:       "playlist not ready",
			err:        newReasonError(model.RPackagerFailed, "playlist not ready after 10s", nil),
			wantReason: model.RPackagerFailed,
			wantDetail: "playlist not ready after 10s",
		},
		{
			name:       "context canceled",
			err:        context.Canceled,
			wantReason: model.RClientStop,
			wantDetail: "context canceled",
		},
		{
			name:       "deadline exceeded",
			err:        context.DeadlineExceeded,
			wantReason: model.RTuneTimeout,
			wantDetail: "deadline exceeded",
		},
		{
			name:           "unmapped error",
			err:            errors.New("some unknown"),
			wantReason:     model.RUnknown,
			wantDetail:     "some unknown",
			detailContains: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, detail := classifyReason(tc.err)
			require.Equal(t, tc.wantReason, reason)
			if tc.wantDetail != "" {
				if tc.detailContains {
					require.True(t, strings.Contains(detail, tc.wantDetail), "detail should contain %q, got %q", tc.wantDetail, detail)
				} else {
					require.Equal(t, tc.wantDetail, detail)
				}
			}
		})
	}
}

func TestClassifyReason_ProcessExit(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 2")
	err := cmd.Run()
	require.Error(t, err)

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Skip("no exec exit error available")
	}

	reason, detail := classifyReason(err)
	require.Equal(t, model.RProcessEnded, reason)
	require.True(t, strings.Contains(detail, "process exit code 2"))
}
