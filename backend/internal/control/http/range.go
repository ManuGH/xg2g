package http

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrInvalidRange = errors.New("invalid range")
	ErrMultiRange   = errors.New("multi-range not supported")
)

// Range represents a byte range [Start, End] (inclusive).
type Range struct {
	Start int64
	End   int64
}

// ParseRange parses a "Range" header and returns a single Range.
// Follows Policy A: Multi-range is strictly rejected with ErrMultiRange.
// size is the total size of the resource.
func ParseRange(header string, size int64) (Range, error) {
	if header == "" {
		return Range{}, ErrInvalidRange
	}

	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return Range{}, ErrInvalidRange
	}

	rangesStr := strings.TrimPrefix(header, prefix)
	if strings.Contains(rangesStr, ",") {
		return Range{}, ErrMultiRange
	}

	parts := strings.SplitN(rangesStr, "-", 2)
	if len(parts) != 2 {
		return Range{}, ErrInvalidRange
	}

	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])

	var r Range

	if startStr == "" {
		// Suffix range: bytes=-500 (last 500 bytes)
		if endStr == "" {
			return Range{}, ErrInvalidRange
		}
		i, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || i <= 0 {
			return Range{}, ErrInvalidRange
		}
		if i > size {
			i = size
		}
		r.Start = size - i
		r.End = size - 1
	} else {
		// Normal range: bytes=0- or bytes=0-500
		i, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil || i < 0 {
			return Range{}, ErrInvalidRange
		}
		if i >= size {
			return Range{}, ErrInvalidRange
		}
		r.Start = i

		if endStr == "" {
			r.End = size - 1
		} else {
			j, err := strconv.ParseInt(endStr, 10, 64)
			if err != nil || j < 0 {
				return Range{}, ErrInvalidRange
			}
			if j < r.Start {
				return Range{}, ErrInvalidRange
			}
			if j >= size {
				j = size - 1
			}
			r.End = j
		}
	}

	return r, nil
}

// FormatContentRange formats the Content-Range header.
func FormatContentRange(r Range, size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.Start, r.End, size)
}

// Format416ContentRange formats the Content-Range header for a 416 response.
func Format416ContentRange(size int64) string {
	return fmt.Sprintf("bytes */%d", size)
}
