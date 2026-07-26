// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import "strings"

// redactedArgValuePlaceholder replaces argument values that carry credentials.
const redactedArgValuePlaceholder = "<redacted>"

// argsWithSecretValuesRedacted returns the ffmpeg argument vector with credential
// carriers masked, so the full command can be logged.
//
// Why this exists: the startup log only carried arg_count, so diagnosing "which
// bitrate/codec did ffmpeg actually get" required reading /proc/<pid>/cmdline on the
// host — impossible after the process is gone, and impossible from a log export.
//
// Two carriers must never reach the log: the value after -headers (it holds an
// Authorization: Basic header for the Enigma2 box) and any URL with userinfo (the
// stream URL gets credentials injected for the relay port).
func argsWithSecretValuesRedacted(args []string) []string {
	out := make([]string, len(args))
	redactNext := false
	for i, arg := range args {
		switch {
		case redactNext:
			out[i] = redactedArgValuePlaceholder
			redactNext = false
		case strings.Contains(arg, "://"):
			out[i] = sanitizeFFmpegLogLine(arg)
		default:
			out[i] = arg
		}
		if strings.EqualFold(arg, "-headers") {
			redactNext = true
		}
	}
	return out
}
