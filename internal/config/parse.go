// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParseTunerSlots parses XG2G_V3_TUNER_SLOTS.
// Supported forms: "0,1,2" and ranges "0..3" or "0-3" (optionally mixed).
func ParseTunerSlots(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil // nil => "no slots configured"
	}

	var out []int
	seen := map[int]struct{}{}

	add := func(v int) error {
		if v < 0 {
			return fmt.Errorf("tuner slot must be >= 0 (got %d)", v)
		}
		if _, ok := seen[v]; ok {
			return nil
		}
		seen[v] = struct{}{}
		out = append(out, v)
		return nil
	}

	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Range: "a..b"
		if strings.Contains(p, "..") {
			ab := strings.Split(p, "..")
			if len(ab) != 2 {
				return nil, fmt.Errorf("invalid tuner slot range: %q", p)
			}
			a, err := strconv.Atoi(strings.TrimSpace(ab[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid tuner slot range start %q: %w", ab[0], err)
			}
			b, err := strconv.Atoi(strings.TrimSpace(ab[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid tuner slot range end %q: %w", ab[1], err)
			}
			if a > b {
				return nil, fmt.Errorf("invalid tuner slot range %q: start > end", p)
			}
			for i := a; i <= b; i++ {
				if err := add(i); err != nil {
					return nil, err
				}
			}
			continue
		}

		// Range: "a-b"
		if strings.Count(p, "-") == 1 && !strings.HasPrefix(p, "-") {
			ab := strings.Split(p, "-")
			a, err := strconv.Atoi(strings.TrimSpace(ab[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid tuner slot range start %q: %w", ab[0], err)
			}
			b, err := strconv.Atoi(strings.TrimSpace(ab[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid tuner slot range end %q: %w", ab[1], err)
			}
			if a > b {
				return nil, fmt.Errorf("invalid tuner slot range %q: start > end", p)
			}
			for i := a; i <= b; i++ {
				if err := add(i); err != nil {
					return nil, err
				}
			}
			continue
		}

		// Single int
		v, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid tuner slot %q: %w", p, err)
		}
		if err := add(v); err != nil {
			return nil, err
		}
	}

	sort.Ints(out)
	return out, nil
}
