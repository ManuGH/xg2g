package playbackprofile

import (
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
)

// CanonicalizeSource normalizes source truth to a deterministic, semantically stable form.
func CanonicalizeSource(in SourceProfile) SourceProfile {
	return ports.CanonicalizeSource(in)
}

// CanonicalizeClient normalizes the client profile for hashing and semantic comparison.
func CanonicalizeClient(in ClientPlaybackProfile) ClientPlaybackProfile {
	return ports.CanonicalizeClient(in)
}

// CanonicalizeServerCapabilities normalizes the executable host capability snapshot.
func CanonicalizeServerCapabilities(in ServerTranscodeCapabilities) ServerTranscodeCapabilities {
	return ports.CanonicalizeServerCapabilities(in)
}

// CanonicalizeHostRuntime normalizes the host runtime snapshot for stable comparison.
func CanonicalizeHostRuntime(in HostRuntimeSnapshot) HostRuntimeSnapshot {
	return ports.CanonicalizeHostRuntime(in)
}

// CanonicalizeTarget normalizes the target output profile for hashing and cache identity.
func CanonicalizeTarget(in TargetPlaybackProfile) TargetPlaybackProfile {
	return ports.CanonicalizeTarget(in)
}
