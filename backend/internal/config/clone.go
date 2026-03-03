// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

// Clone returns an alias-free deep copy of AppConfig.
// Only reference types (maps/slices) are cloned; nested structs are copied by value.
func Clone(in AppConfig) AppConfig {
	out := in

	// --- Top-level slices ---
	out.AllowedOrigins = cloneStringSlice(in.AllowedOrigins)
	out.RateLimitWhitelist = cloneStringSlice(in.RateLimitWhitelist)
	out.APITokenScopes = cloneStringSlice(in.APITokenScopes)
	out.PlaybackDecisionPreviousKeys = cloneStringSlice(in.PlaybackDecisionPreviousKeys)
	out.RecordingPathMappings = cloneRecordingPathMappings(in.RecordingPathMappings)

	// --- Maps (preserve nil) ---
	if in.RecordingRoots != nil {
		out.RecordingRoots = cloneStringMap(in.RecordingRoots)
	} else {
		out.RecordingRoots = nil
	}

	// --- Engine nested slice ---
	out.Engine.TunerSlots = cloneIntSlice(in.Engine.TunerSlots)

	// --- Library deep copy ---
	out.Library.Roots = cloneLibraryRoots(in.Library.Roots)

	// --- API tokens: deep slice-of-structs, plus deep Scopes ---
	out.APITokens = cloneScopedTokens(in.APITokens)

	// --- Network nested allowlists (slices) ---
	out.Network.Outbound.Allow.Hosts = cloneStringSlice(in.Network.Outbound.Allow.Hosts)
	out.Network.Outbound.Allow.CIDRs = cloneStringSlice(in.Network.Outbound.Allow.CIDRs)
	out.Network.Outbound.Allow.Schemes = cloneStringSlice(in.Network.Outbound.Allow.Schemes)
	out.Network.Outbound.Allow.Ports = cloneIntSlice(in.Network.Outbound.Allow.Ports)

	return out
}

func cloneStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneIntSlice(in []int) []int {
	if in == nil {
		return nil
	}
	out := make([]int, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneScopedTokens(in []ScopedToken) []ScopedToken {
	if in == nil {
		return nil
	}
	out := make([]ScopedToken, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Scopes = cloneStringSlice(in[i].Scopes)
	}
	return out
}

func cloneLibraryRoots(in []LibraryRootConfig) []LibraryRootConfig {
	if in == nil {
		return nil
	}
	out := make([]LibraryRootConfig, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].IncludeExt = cloneStringSlice(in[i].IncludeExt)
	}
	return out
}

func cloneRecordingPathMappings(in []RecordingPathMapping) []RecordingPathMapping {
	if in == nil {
		return nil
	}
	out := make([]RecordingPathMapping, len(in))
	copy(out, in)
	return out
}
