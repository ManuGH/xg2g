// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import "errors"

var (
	// ErrUnknownConfigField classifies strict YAML parse failures caused by unknown keys.
	// Use errors.Is(err, ErrUnknownConfigField) instead of string matching.
	ErrUnknownConfigField = errors.New("unknown config field")
)
