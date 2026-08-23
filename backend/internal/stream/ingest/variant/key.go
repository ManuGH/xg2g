// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import "fmt"

// AudioVariantKey uniquely and stably identifies an audio variant stream.
// It incorporates stream identity, exact audio PID, track language/role,
// and target encoding parameters to guarantee that different tracks or formats
// are never conflated while allowing identical client demands to coalesce.
type AudioVariantKey struct {
	StreamFingerprint string // Stream identifier / PMT generation
	ProgramNumber     uint16 // Target MPEG-TS Program Number
	AudioPID          uint16 // Concrete source audio elementary stream PID
	Language          string // e.g. "deu", "ger", "mis", "und"
	Role              string // e.g. "main", "clean_effects", "audio_description"
	TargetCodec       string // e.g. "aac"
	SampleRate        int    // e.g. 48000
	Channels          int    // e.g. 2
	BitrateKbps       int    // e.g. 160 or 192
}

// String provides a human-readable and deterministic key representation.
func (k AudioVariantKey) String() string {
	return fmt.Sprintf("%s:prog%d:pid0x%04x:%s:%s->%s@%dk_%dHz_ch%d",
		k.StreamFingerprint, k.ProgramNumber, k.AudioPID, k.Language, k.Role, k.TargetCodec, k.BitrateKbps, k.SampleRate, k.Channels)
}
