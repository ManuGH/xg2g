// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import "testing"

// descriptor assembles one PMT descriptor: tag, length, body.
func descriptor(tag byte, body ...byte) []byte {
	return append([]byte{tag, byte(len(body))}, body...)
}

// ac3Desc builds an AC-3 descriptor whose component_type_flag is set, so the
// component type is the byte that follows the flags.
func ac3Desc(componentType byte) []byte {
	return descriptor(descriptorAC3, ac3ComponentTypeFlagBit, componentType)
}

// aacDesc builds an AAC descriptor: profile_and_level, the AAC_type_flag byte,
// then AAC_type.
func aacDesc(aacType byte) []byte {
	return descriptor(descriptorAAC, 0x50, aacTypeFlagBit, aacType)
}

func TestAudioChannelsFromDescriptors_AC3(t *testing.T) {
	tests := []struct {
		name          string
		componentType byte
		wantChannels  int
		wantMulti     bool
	}{
		{"mono", ac3ChannelsMono, 1, false},
		{"dual mono is two carried channels", ac3ChannelsDualMono, 2, false},
		{"stereo", ac3ChannelsStereo, 2, false},
		{"dolby surround encoded stereo is still two channels", ac3ChannelsStereoDsur, 2, false},
		{"multichannel declares no count", ac3ChannelsMultiFirst, 0, true},
		{"multichannel upper value", ac3ChannelsMultiLast, 0, true},
		{"reserved names nothing", 0x07, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AudioChannelsFromDescriptors(ac3Desc(tc.componentType))

			if got.Channels != tc.wantChannels {
				t.Errorf("Channels = %d, want %d", got.Channels, tc.wantChannels)
			}
			if got.Multichannel != tc.wantMulti {
				t.Errorf("Multichannel = %v, want %v", got.Multichannel, tc.wantMulti)
			}
			if !got.HasComponentType {
				t.Error("HasComponentType = false, want true: the descriptor carried one")
			}
			if got.ComponentType != tc.componentType {
				t.Errorf("ComponentType = %#x, want %#x", got.ComponentType, tc.componentType)
			}
		})
	}
}

// The service type occupies the high bits of the same byte. Reading the whole
// byte as a channel count instead of masking the low nibble would turn every
// non-complete-main service into a wrong answer.
func TestAudioChannelsFromDescriptors_IgnoresServiceTypeBits(t *testing.T) {
	// full_service_flag set, service_type 5 (commentary), channels = stereo.
	const componentType = 0x80 | (5 << 4) | ac3ChannelsStereo

	got := AudioChannelsFromDescriptors(ac3Desc(componentType))

	if got.Channels != 2 {
		t.Errorf("Channels = %d, want 2", got.Channels)
	}
	if got.ComponentType != componentType {
		t.Errorf("ComponentType = %#x, want %#x: the raw byte must survive for the policy layer",
			got.ComponentType, componentType)
	}
}

func TestAudioChannelsFromDescriptors_AAC(t *testing.T) {
	tests := []struct {
		name         string
		aacType      byte
		wantChannels int
		wantMulti    bool
	}{
		{"aac mono", aacTypeMono, 1, false},
		{"aac stereo", aacTypeStereo, 2, false},
		{"aac surround declares no count", aacTypeSurround, 0, true},
		{"he-aac mono", aacTypeHEMono, 1, false},
		{"he-aac stereo", aacTypeHEStereo, 2, false},
		{"he-aac surround declares no count", aacTypeHESurr, 0, true},
		{"audio description names no channel count", 0x40, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AudioChannelsFromDescriptors(aacDesc(tc.aacType))

			if got.Channels != tc.wantChannels {
				t.Errorf("Channels = %d, want %d", got.Channels, tc.wantChannels)
			}
			if got.Multichannel != tc.wantMulti {
				t.Errorf("Multichannel = %v, want %v", got.Multichannel, tc.wantMulti)
			}
		})
	}
}

// Absent, truncated and flag-less descriptors must all report unknown rather
// than a plausible default. A wrong number here reaches the encoder as a real
// channel count; "unknown" lets the policy layer keep its own fallback.
func TestAudioChannelsFromDescriptors_UnknownCases(t *testing.T) {
	tests := []struct {
		name        string
		descriptors []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"no audio descriptor at all", descriptor(0x0A, 'd', 'e', 'u', 0x00)},
		{"ac3 without component_type_flag", descriptor(descriptorAC3, 0x00)},
		{"ac3 flag set but body truncated", descriptor(descriptorAC3, ac3ComponentTypeFlagBit)},
		{"aac without AAC_type_flag", descriptor(descriptorAAC, 0x50, 0x00)},
		{"aac flag set but body truncated", descriptor(descriptorAAC, 0x50, aacTypeFlagBit)},
		{"mp2 declares nothing in a descriptor", descriptor(0x0A, 'e', 'n', 'g', 0x00)},
		{"dts is not declared here", descriptor(descriptorDTS, 0x01, 0x02)},
		{"length runs past the buffer", []byte{descriptorAC3, 0x7F, 0x80}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AudioChannelsFromDescriptors(tc.descriptors)

			if got.Known() {
				t.Errorf("Known() = true (%+v), want unknown", got)
			}
			if got.Channels != 0 {
				t.Errorf("Channels = %d, want 0", got.Channels)
			}
		})
	}
}

// A descriptor loop carries several descriptors; the audio one is rarely first.
func TestAudioChannelsFromDescriptors_WalksPastOtherDescriptors(t *testing.T) {
	descriptors := append(
		descriptor(0x0A, 'd', 'e', 'u', 0x00), // ISO 639 language
		ac3Desc(ac3ChannelsStereo)...,
	)

	got := AudioChannelsFromDescriptors(descriptors)

	if got.Channels != 2 {
		t.Errorf("Channels = %d, want 2", got.Channels)
	}
}

// Known() is the distinction the policy layer branches on, so a stereo
// declaration and a silent stream must not look alike.
func TestAudioChannelDeclaration_Known(t *testing.T) {
	tests := []struct {
		name string
		decl AudioChannelDeclaration
		want bool
	}{
		{"zero value", AudioChannelDeclaration{}, false},
		{"counted", AudioChannelDeclaration{Channels: 2}, true},
		{"multichannel without a count", AudioChannelDeclaration{Multichannel: true}, true},
		{"component type but no channel meaning", AudioChannelDeclaration{ComponentType: 0x40, HasComponentType: true}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.decl.Known(); got != tc.want {
				t.Errorf("Known() = %v, want %v", got, tc.want)
			}
		})
	}
}
