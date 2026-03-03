// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"fmt"
	"strings"
)

const correlationIDMaxLen = 64

// NormalizeCorrelationID validates and normalizes a correlation ID.
// It returns an empty string if the input is empty after trimming.
func NormalizeCorrelationID(id string) (string, error) {
	clean := strings.TrimSpace(id)
	if clean == "" {
		return "", nil
	}
	if len(clean) > correlationIDMaxLen {
		return "", fmt.Errorf("correlationId too long")
	}
	for i := 0; i < len(clean); i++ {
		ch := clean[i]
		if ch > 0x7e {
			return "", fmt.Errorf("correlationId must be ASCII")
		}
		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return "", fmt.Errorf("correlationId has invalid characters")
	}
	return clean, nil
}
