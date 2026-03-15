// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import (
	"sort"

	"github.com/ManuGH/xg2g/internal/normalize"
)

// CanonicalizeSource normalizes source truth to a deterministic, semantically stable form.
func CanonicalizeSource(in SourceProfile) SourceProfile {
	out := in
	out.Container = normalize.Token(out.Container)
	out.VideoCodec = normalize.Token(out.VideoCodec)
	out.AudioCodec = normalize.Token(out.AudioCodec)
	if out.BitrateKbps < 0 {
		out.BitrateKbps = 0
	}
	if out.Width < 0 {
		out.Width = 0
	}
	if out.Height < 0 {
		out.Height = 0
	}
	if out.FPS < 0 {
		out.FPS = 0
	}
	if out.AudioChannels < 0 {
		out.AudioChannels = 0
	}
	if out.AudioBitrateKbps < 0 {
		out.AudioBitrateKbps = 0
	}
	return out
}

// CanonicalizeClient normalizes the client profile for hashing and semantic comparison.
func CanonicalizeClient(in ClientPlaybackProfile) ClientPlaybackProfile {
	out := in
	out.DeviceType = normalize.Token(out.DeviceType)
	out.PlaybackEngine = normalize.Token(out.PlaybackEngine)
	out.Containers = canonicalStringSet(out.Containers)
	out.VideoCodecs = canonicalStringSet(out.VideoCodecs)
	out.AudioCodecs = canonicalStringSet(out.AudioCodecs)
	out.HLSPackaging = canonicalStringSet(out.HLSPackaging)

	if out.Containers == nil {
		out.Containers = []string{}
	}
	if out.VideoCodecs == nil {
		out.VideoCodecs = []string{}
	}
	if out.AudioCodecs == nil {
		out.AudioCodecs = []string{}
	}
	if out.HLSPackaging == nil {
		out.HLSPackaging = []string{}
	}
	if out.MaxVideo != nil {
		mv := *out.MaxVideo
		if mv.Width < 0 {
			mv.Width = 0
		}
		if mv.Height < 0 {
			mv.Height = 0
		}
		if mv.FPS < 0 {
			mv.FPS = 0
		}
		if mv.Width == 0 && mv.Height == 0 && mv.FPS == 0 {
			out.MaxVideo = nil
		} else {
			out.MaxVideo = &mv
		}
	}
	if out.AllowTranscode != nil {
		v := *out.AllowTranscode
		out.AllowTranscode = &v
	}
	return out
}

// CanonicalizeServerCapabilities normalizes the executable host capability snapshot.
func CanonicalizeServerCapabilities(in ServerTranscodeCapabilities) ServerTranscodeCapabilities {
	out := in
	out.HardwareVideoCodec = canonicalStringSet(out.HardwareVideoCodec)
	if out.HardwareVideoCodec == nil {
		out.HardwareVideoCodec = []string{}
	}
	return out
}

// CanonicalizeTarget normalizes the target output profile for hashing and cache identity.
func CanonicalizeTarget(in TargetPlaybackProfile) TargetPlaybackProfile {
	out := in
	out.Container = normalize.Token(out.Container)
	out.Packaging = Packaging(normalize.Token(string(out.Packaging)))
	if out.HWAccel == "" {
		out.HWAccel = HWAccelNone
	} else {
		out.HWAccel = HWAccel(normalize.Token(string(out.HWAccel)))
	}

	out.Video.Mode = MediaMode(normalize.Token(string(out.Video.Mode)))
	out.Video.Codec = normalize.Token(out.Video.Codec)
	if out.Video.BitrateKbps < 0 {
		out.Video.BitrateKbps = 0
	}
	if out.Video.Width < 0 {
		out.Video.Width = 0
	}
	if out.Video.Height < 0 {
		out.Video.Height = 0
	}
	if out.Video.FPS < 0 {
		out.Video.FPS = 0
	}

	out.Audio.Mode = MediaMode(normalize.Token(string(out.Audio.Mode)))
	out.Audio.Codec = normalize.Token(out.Audio.Codec)
	if out.Audio.Channels < 0 {
		out.Audio.Channels = 0
	}
	if out.Audio.BitrateKbps < 0 {
		out.Audio.BitrateKbps = 0
	}
	if out.Audio.SampleRate < 0 {
		out.Audio.SampleRate = 0
	}

	out.HLS.SegmentContainer = normalize.Token(out.HLS.SegmentContainer)
	if out.HLS.SegmentSeconds < 0 {
		out.HLS.SegmentSeconds = 0
	}
	return out
}

func canonicalStringSet(in []string) []string {
	if in == nil {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		token := normalize.Token(raw)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	sort.Strings(out)
	return out
}
