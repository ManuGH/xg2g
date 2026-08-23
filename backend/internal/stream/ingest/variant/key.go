// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package variant

import "fmt"

// StreamVariantKey uniquely and stably identifies a stream variant (video and/or audio transformation).
// It incorporates stream identity, target program, video and audio parameters, and processing policies
// to guarantee that distinct transformations are never conflated while allowing identical client demands to coalesce.
type StreamVariantKey struct {
	StreamFingerprint string // Stream identifier / PMT generation
	ProgramNumber     uint16 // Target MPEG-TS Program Number

	// Video Transformation
	SourceVideoCodec string // e.g. "mpeg2", "h264", "hevc"
	TargetVideoCodec string // e.g. "copy", "h264"
	Width            int    // 0 = keep source
	Height           int    // 0 = keep source
	FPS              int    // e.g. 50
	ScanPolicy       string // "passthrough", "deinterlace_50p", "progressive"
	PixFmt           string // e.g. "yuv420p"

	// Audio Transformation
	AudioPID         uint16 // Concrete source audio elementary stream PID
	SourceAudioCodec string // e.g. "mp2", "ac3"
	TargetAudioCodec string // e.g. "copy", "aac"
	Language         string // e.g. "deu", "ger", "mis", "und"
	Role             string // e.g. "main", "clean_effects", "audio_description"
	SampleRate       int    // e.g. 48000
	Channels         int    // e.g. 2
	BitrateKbps      int    // e.g. 160 or 192
}

// AudioVariantKey is an alias to StreamVariantKey for backward compatibility.
type AudioVariantKey = StreamVariantKey

// String provides a human-readable and deterministic key representation.
func (k StreamVariantKey) String() string {
	targetV := k.TargetVideoCodec
	if targetV == "" {
		targetV = "copy"
	}
	targetA := k.TargetAudioCodec
	if targetA == "" {
		targetA = "copy"
	}
	scan := k.ScanPolicy
	if scan == "" {
		scan = "passthrough"
	}

	return fmt.Sprintf("%s:prog%d:v[%s->%s:%s]:a[pid0x%04x:%s:%s->%s@%dk_%dHz_ch%d]",
		k.StreamFingerprint, k.ProgramNumber,
		k.SourceVideoCodec, targetV, scan,
		k.AudioPID, k.Language, k.Role, targetA,
		k.BitrateKbps, k.SampleRate, k.Channels)
}
