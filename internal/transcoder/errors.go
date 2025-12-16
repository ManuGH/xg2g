package transcoder

import "errors"

var (
	// ErrOutputTooSmall is returned when the provided output buffer is not large enough
	// to hold the remuxed data. The caller should retry with a larger buffer.
	ErrOutputTooSmall = errors.New("output buffer too small")

	// ErrInvalidInput is returned when the input data is invalid (e.g. empty or overlaps with output).
	ErrInvalidInput = errors.New("invalid input")

	// ErrTranscoderUnavailable is returned when the native transcoder is not available
	// (e.g. not built with cgo/tags, or handle is closed).
	ErrTranscoderUnavailable = errors.New("transcoder unavailable")
)
