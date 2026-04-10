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
		}
	}
	return out
}
