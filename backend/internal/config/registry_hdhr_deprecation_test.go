// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"strings"
	"testing"
)

// TestHDHR_KeysAreDeprecatedNotActive is the drift guard for the removed HDHomeRun
// emulation. Its endpoints left with the legacy HTTP surface and nothing announces
// the device any more, so every hdhr.* key is inert. Advertising one as Active
// would tell an operator the emulation works when it does nothing.
//
// The keys must also stay registered: the loader rejects unknown keys, so dropping
// them would stop existing configs that still carry an hdhr section from loading.
func TestHDHR_KeysAreDeprecatedNotActive(t *testing.T) {
	reg, err := GetRegistry()
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}

	want := []string{
		"hdhr.enabled",
		"hdhr.deviceId",
		"hdhr.friendlyName",
		"hdhr.modelNumber",
		"hdhr.firmwareName",
		"hdhr.baseUrl",
		"hdhr.tunerCount",
		"hdhr.plexForceHls",
	}
	for _, path := range want {
		entry, ok := reg.ByPath[path]
		if !ok {
			t.Errorf("%s missing from registry: it must stay registered so configs that still set it keep loading", path)
			continue
		}
		if entry.Status != StatusDeprecated {
			t.Errorf("%s Status = %q, want %q: the HDHomeRun emulation is removed and the key is inert", path, entry.Status, StatusDeprecated)
		}
	}

	// No hdhr key may be added back as a working option without the emulation
	// coming back with it.
	for path, entry := range reg.ByPath {
		if strings.HasPrefix(path, "hdhr.") && entry.Status != StatusDeprecated {
			t.Errorf("%s Status = %q, want %q", path, entry.Status, StatusDeprecated)
		}
	}
}
