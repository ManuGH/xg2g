package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MakeTimerID creates a stable, URL-safe ID from a timer's unique triplet:
// ServiceRef, Begin time, and End time.
// Format: base64url(serviceRef|begin|end)
func MakeTimerID(serviceRef string, begin, end int64) string {
	raw := fmt.Sprintf("%s|%d|%d", serviceRef, begin, end)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(raw))
}

// ParseTimerID decodes a timerId back into its components.
// It strictly verifies the format and ensures begin < end.
func ParseTimerID(timerId string) (serviceRef string, begin, end int64, err error) {
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(timerId)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid base64: %w", err)
	}

	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 {
		return "", 0, 0, errors.New("invalid timer ID format: expected 3 parts")
	}

	serviceRef = parts[0]
	if serviceRef == "" {
		return "", 0, 0, errors.New("invalid timer ID: empty serviceRef")
	}

	begin, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid begin time: %w", err)
	}

	end, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid end time: %w", err)
	}

	if begin >= end {
		return "", 0, 0, errors.New("invalid timer: begin time must be before end time")
	}

	return serviceRef, begin, end, nil
}
