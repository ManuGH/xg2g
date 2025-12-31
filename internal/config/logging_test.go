// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/log"
)

func TestConfigLoggerDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("config logger panicked: %v", r)
		}
	}()

	logger := log.WithComponent("config")
	logger.Warn().Msg("logger sanity check")
}
