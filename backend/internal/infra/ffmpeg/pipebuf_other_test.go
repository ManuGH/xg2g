//go:build !linux

package ffmpeg

import "errors"

// errPipeResizeUnsupported signals that the platform cannot enlarge a pipe buffer (e.g.
// macOS has no F_SETPIPE_SZ and caps pipes at ~64KB). The truncation reproducer skips here.
var errPipeResizeUnsupported = errors.New("pipe buffer resize unsupported on this platform")

func growPipeBuffer(_ uintptr, _ int) error { return errPipeResizeUnsupported }
