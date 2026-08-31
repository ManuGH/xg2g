// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build !race

package mediafacts

// The ordinary build: see race_on_test.go.
const raceDetectorEnabled = false
