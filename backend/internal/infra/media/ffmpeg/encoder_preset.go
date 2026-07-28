// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strconv"
	"strings"
)

// svtAV1PresetForName maps the x264/x265 preset vocabulary to SVT-AV1's numeric
// speed scale (0 slowest … 13 fastest).
//
// libsvtav1 rejects the names outright — verified against ffmpeg 8.1.1:
//
//	-c:v libsvtav1 -preset superfast
//	  [libsvtav1] Undefined constant or missing '(' in 'superfast'
//	  [libsvtav1] Unable to parse "preset" option value "superfast"  -> exit 234
//
// The CPU encode path shares its preset vocabulary with libx264/libx265, so an AV1
// session without a usable GPU died during startup. That is precisely the
// GPU-demotion fallback, i.e. the path taken when things are already going wrong.
var svtAV1PresetForName = map[string]string{
	"ultrafast": "12",
	"superfast": "11",
	"veryfast":  "10",
	"faster":    "9",
	"fast":      "8",
	"medium":    "7",
	"slow":      "6",
	"slower":    "5",
	"veryslow":  "4",
	"placebo":   "2",
}

// svtAV1DefaultPreset is the fallback for an unknown name: fast enough for realtime
// live encoding on a CPU, without giving up all quality.
const svtAV1DefaultPreset = "8"

// encoderPreset adapts a preset value to the vocabulary the chosen encoder accepts.
// Every encoder other than libsvtav1 (libx264, libx265, and the hardware encoders
// that take a preset at all) understands the names as-is.
func encoderPreset(codec, preset string) string {
	if !strings.EqualFold(strings.TrimSpace(codec), "libsvtav1") {
		return preset
	}
	trimmed := strings.TrimSpace(preset)
	// A numeric preset is already in SVT-AV1's vocabulary; pass it through so an
	// operator can pin an exact speed level.
	if _, err := strconv.Atoi(trimmed); err == nil {
		return trimmed
	}
	if mapped, ok := svtAV1PresetForName[strings.ToLower(trimmed)]; ok {
		return mapped
	}
	return svtAV1DefaultPreset
}
