package config

import "sort"

// DeprecatedRegistryEntries returns active registry entries that are marked deprecated.
func DeprecatedRegistryEntries() []ConfigEntry {
	registry, err := GetRegistry()
	if err != nil || registry == nil {
		return nil
	}

	entries := make([]ConfigEntry, 0)
	for _, entry := range registry.ByPath {
		if entry.Status == StatusDeprecated {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

// DeprecatedEnvKeys returns deprecated operator env keys from the registry SSOT.
func DeprecatedEnvKeys() []string {
	entries := DeprecatedRegistryEntries()
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Env == "" {
			continue
		}
		out = append(out, entry.Env)
	}
	return out
}

// DeprecatedFileConfigPaths returns explicitly configured deprecated YAML paths.
func DeprecatedFileConfigPaths(cfg FileConfig) []string {
	entries := DeprecatedRegistryEntries()
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		switch entry.Path {
		case "enigma2.streamPort":
			if cfg.Enigma2.StreamPort != nil {
				out = append(out, entry.Path)
			}
		case "enigma2.useWebIFStreams":
			if cfg.Enigma2.UseWebIF != nil {
				out = append(out, entry.Path)
			}
		case "enigma2.fallbackTo8001":
			if cfg.Enigma2.FallbackTo8001 != nil {
				out = append(out, entry.Path)
			}
		}
	}
	return out
}

// EndOfLifeFor returns the announced removal date for a deprecated surface,
// addressed by either its registry path or its environment variable name.
// The boolean is false when the surface is unknown, still active, or has no
// announced date.
func EndOfLifeFor(pathOrEnv string) (string, bool) {
	for _, entry := range DeprecatedRegistryEntries() {
		if entry.Path == pathOrEnv || (entry.Env != "" && entry.Env == pathOrEnv) {
			return entry.EndOfLife()
		}
	}
	return "", false
}

// DescribeDeprecatedSurface renders a surface with its end-of-life date when
// one has been announced, e.g. "enigma2.streamPort (removal after 2027-02-01)".
// Surfaces without an announced date are returned unchanged.
func DescribeDeprecatedSurface(pathOrEnv string) string {
	if date, ok := EndOfLifeFor(pathOrEnv); ok {
		return pathOrEnv + " (removal after " + date + ")"
	}
	return pathOrEnv
}
