// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build race

package mediafacts

// Whether this binary was built with -race. There is no testing.RaceEnabled, so
// the build tag is the only thing a test can ask; see race_off_test.go for the
// other half. Nothing here changes behaviour under the detector - it exists so a
// wall-clock measurement can tell that it is being taken through one.
const raceDetectorEnabled = true
