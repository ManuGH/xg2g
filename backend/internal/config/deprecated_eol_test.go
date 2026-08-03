// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"regexp"
	"strings"
	"testing"
)

var eolDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// The relay input options must carry an announced end-of-life date so an
// operator can plan the move off that path.
func TestRelayInputSurfacesAnnounceEndOfLife(t *testing.T) {
	want := []string{
		"enigma2.streamPort",
		"enigma2.useWebIFStreams",
		"enigma2.fallbackTo8001",
	}

	for _, path := range want {
		entry, ok := registryEntry(t, path)
		if !ok {
			t.Fatalf("registry entry %q not found", path)
		}
		if entry.Status != StatusDeprecated {
			t.Errorf("%s: status = %q, want %q", path, entry.Status, StatusDeprecated)
		}
		date, ok := entry.EndOfLife()
		if !ok {
			t.Errorf("%s: no end-of-life date announced", path)
			continue
		}
		if date != RelayInputRemoveAfter {
			t.Errorf("%s: end-of-life = %q, want %q", path, date, RelayInputRemoveAfter)
		}
	}

	if !eolDatePattern.MatchString(RelayInputRemoveAfter) {
		t.Errorf("RelayInputRemoveAfter = %q, want YYYY-MM-DD", RelayInputRemoveAfter)
	}
}

// EndOfLife is metadata about deprecated options only; an active option must
// never report a removal date even if one were set by mistake.
func TestEndOfLifeOnlyAppliesToDeprecatedEntries(t *testing.T) {
	active := ConfigEntry{Status: StatusActive, RemoveAfter: "2027-02-01"}
	if date, ok := active.EndOfLife(); ok {
		t.Errorf("active entry reported end-of-life %q, want none", date)
	}

	deprecatedNoDate := ConfigEntry{Status: StatusDeprecated}
	if date, ok := deprecatedNoDate.EndOfLife(); ok {
		t.Errorf("dateless entry reported end-of-life %q, want none", date)
	}

	deprecated := ConfigEntry{Status: StatusDeprecated, RemoveAfter: "2027-02-01"}
	date, ok := deprecated.EndOfLife()
	if !ok || date != "2027-02-01" {
		t.Errorf("EndOfLife() = (%q, %v), want (\"2027-02-01\", true)", date, ok)
	}
}

// Operator-facing output must carry the date, addressable by registry path and
// by env key, and must leave unknown or undated surfaces untouched.
func TestDescribeDeprecatedSurfaceCarriesDate(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"by path", "enigma2.fallbackTo8001", "enigma2.fallbackTo8001 (removal after " + RelayInputRemoveAfter + ")"},
		{"by env", "XG2G_E2_FALLBACK_TO_8001", "XG2G_E2_FALLBACK_TO_8001 (removal after " + RelayInputRemoveAfter + ")"},
		{"unknown surface unchanged", "does.not.exist", "does.not.exist"},
		{"active surface unchanged", "enigma2.baseUrl", "enigma2.baseUrl"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DescribeDeprecatedSurface(tc.input); got != tc.want {
				t.Errorf("DescribeDeprecatedSurface(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// An explicitly configured relay option must show up as a deprecated YAML path
// so the upgrade preflight can block on it.
func TestFallbackTo8001ReportedAsDeprecatedFilePath(t *testing.T) {
	enabled := true
	cfg := FileConfig{}
	cfg.Enigma2.FallbackTo8001 = &enabled

	paths := DeprecatedFileConfigPaths(cfg)
	found := false
	for _, p := range paths {
		if p == "enigma2.fallbackTo8001" {
			found = true
		}
	}
	if !found {
		t.Errorf("DeprecatedFileConfigPaths() = %v, want it to contain enigma2.fallbackTo8001", paths)
	}

	// Unset stays silent — deprecation must not nag deployments that never
	// opted into the option.
	if paths := DeprecatedFileConfigPaths(FileConfig{}); len(paths) != 0 {
		t.Errorf("DeprecatedFileConfigPaths(empty) = %v, want none", paths)
	}
}

// registryEntry looks up a registry entry by user-facing path.
func registryEntry(t *testing.T, path string) (ConfigEntry, bool) {
	t.Helper()
	registry, err := GetRegistry()
	if err != nil || registry == nil {
		t.Fatalf("GetRegistry() failed: %v", err)
	}
	entry, ok := registry.ByPath[strings.TrimSpace(path)]
	return entry, ok
}
