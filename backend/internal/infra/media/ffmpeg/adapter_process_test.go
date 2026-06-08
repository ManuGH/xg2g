// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// A normal user stop or watchdog termination kills ffmpeg (procErr != nil) and
// may carry a latched transient vaapi/nvenc warning line, but that is NOT an
// encoder failure and must not feed the sticky GPU->CPU demotion counter.
func TestShouldRecordHWRuntimeFailure(t *testing.T) {
	exitErr := errors.New("exit status 1")
	const failLine = "vaapi: encode failed"

	cases := []struct {
		name        string
		naturalExit bool
		procErr     error
		failureLine string
		want        bool
	}{
		{"natural non-zero exit with failure line records", true, exitErr, failLine, true},
		{"natural clean exit (code 0) does not record", true, nil, failLine, false},
		{"no failure line never records", true, exitErr, "", false},
		{"user-initiated stop does not record", false, exitErr, failLine, false},
		{"watchdog stall termination does not record", false, exitErr, failLine, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, shouldRecordHWRuntimeFailure(tc.naturalExit, tc.procErr, tc.failureLine))
		})
	}
}
