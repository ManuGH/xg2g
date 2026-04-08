// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package playbackprofile

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
)

// MediaMode describes how a specific media stream should be handled.
type MediaMode = ports.MediaMode

const (
	MediaModeUnknown   = ports.MediaModeUnknown
	MediaModeCopy      = ports.MediaModeCopy
	MediaModeTranscode = ports.MediaModeTranscode
	MediaModeDisabled  = ports.MediaModeDisabled
)

// Packaging describes the outer transport/container packaging for playback.
type Packaging = ports.Packaging

const (
	PackagingUnknown = ports.PackagingUnknown
	PackagingTS      = ports.PackagingTS
	PackagingFMP4    = ports.PackagingFMP4
	PackagingMP4     = ports.PackagingMP4
)

// HWAccel describes the acceleration path chosen for transcoding.
type HWAccel = ports.HWAccel

const (
	HWAccelUnknown = ports.HWAccelUnknown
	HWAccelNone    = ports.HWAccelNone
	HWAccelVAAPI   = ports.HWAccelVAAPI
	HWAccelNVENC   = ports.HWAccelNVENC
)

// VideoConstraints captures upper bounds provided by the client.
type VideoConstraints = ports.VideoConstraints

// SourceProfile captures truthful source media properties.
type SourceProfile = ports.SourceProfile

// ClientPlaybackProfile describes the effective playback path on the client.
type ClientPlaybackProfile = ports.ClientPlaybackProfile

// ServerTranscodeCapabilities describes what the running xg2g host can execute.
type ServerTranscodeCapabilities = ports.ServerTranscodeCapabilities

// HostCPUSnapshot captures read-only runtime CPU load context for playback decisions.
type HostCPUSnapshot = ports.HostCPUSnapshot

// HostConcurrencySnapshot captures read-only runtime concurrency context for playback decisions.
type HostConcurrencySnapshot = ports.HostConcurrencySnapshot

// HostRuntimeSnapshot combines static executable capabilities and current runtime pressure inputs.
type HostRuntimeSnapshot = ports.HostRuntimeSnapshot

// VideoTarget describes the selected output video path.
type VideoTarget = ports.VideoTarget

// AudioTarget describes the selected output audio path.
type AudioTarget = ports.AudioTarget

// HLSTarget carries HLS-specific delivery choices.
type HLSTarget = ports.HLSTarget

// TargetPlaybackProfile is the concrete output profile later consumed by the builder and cache.
type TargetPlaybackProfile = ports.TargetPlaybackProfile
