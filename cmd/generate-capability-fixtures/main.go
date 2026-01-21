package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
)

// profiles is the SSOT list of profiles for fixture generation.
// This MUST match the profiles tested in TestInvariant6_AllProfilesMatchFixtures.
var profiles = []string{
	"android_tv",
	"stb_enigma2",
	"tvos",
	"vlc_desktop",
	"web_conservative",
}

// FixtureCaps represents the fixture JSON format (snake_case for backwards compat).
type FixtureCaps struct {
	CapabilitiesVersion int      `json:"capabilities_version"`
	Containers          []string `json:"containers"`
	VideoCodecs         []string `json:"video_codecs"`
	AudioCodecs         []string `json:"audio_codecs"`
	SupportsHLS         bool     `json:"supports_hls"`
}

type Manifest struct {
	Version string            `json:"version"`
	Hashes  map[string]string `json:"hashes"` // file -> sha256 hex
}

func main() {
	var (
		fixturesDir  = flag.String("fixtures", "fixtures/capabilities", "directory containing capability fixtures")
		manifestPath = flag.String("manifest", "fixtures/GOVERNANCE_CAPABILITIES_BASELINE.json", "output manifest path")
		check        = flag.Bool("check", false, "check mode: do not modify, fail on drift")
	)
	flag.Parse()

	// Ensure directories exist in write mode
	if !*check {
		must(os.MkdirAll(*fixturesDir, 0o755))
		must(os.MkdirAll(filepath.Dir(*manifestPath), 0o755))
	}

	hashes := make(map[string]string, len(profiles))
	ctx := context.Background()

	for _, profile := range profiles {
		// Generate from runtime resolver (SSOT)
		caps := recordings.ResolveCapabilities(ctx, "anonymous", "v3.1", profile, nil, nil)
		caps = capabilities.CanonicalizeCapabilities(caps)

		// Convert to fixture format (snake_case JSON)
		fixture := FixtureCaps{
			CapabilitiesVersion: caps.CapabilitiesVersion,
			Containers:          caps.Containers,
			VideoCodecs:         caps.VideoCodecs,
			AudioCodecs:         caps.AudioCodecs,
			SupportsHLS:         caps.SupportsHLS,
		}

		// Ensure slices are sorted for canonical output
		sort.Strings(fixture.Containers)
		sort.Strings(fixture.VideoCodecs)
		sort.Strings(fixture.AudioCodecs)

		canonicalJSON, err := marshalCanonical(fixture)
		must(err)

		filePath := filepath.Join(*fixturesDir, profile+".json")
		fileName := profile + ".json"

		if *check {
			// Read existing fixture and compare
			existing, err := os.ReadFile(filePath)
			if err != nil {
				fail(fmt.Sprintf("fixture missing: %s (run generator without --check)", filePath))
			}
			if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(canonicalJSON)) {
				fail(fmt.Sprintf("fixture drift: %s (run generator without --check)", filePath))
			}
		} else {
			// Write fixture
			must(os.WriteFile(filePath, canonicalJSON, 0o644))
		}

		sum := sha256.Sum256(canonicalJSON)
		hashes[fileName] = hex.EncodeToString(sum[:])
	}

	manifest := Manifest{
		Version: "p7-capabilities-baseline-v1",
		Hashes:  hashes,
	}
	manifestJSON, err := marshalCanonical(manifest)
	must(err)

	if *check {
		// compare with existing manifest
		existing, err := os.ReadFile(*manifestPath)
		if err != nil {
			fail("manifest missing (run generator without --check): " + *manifestPath)
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(manifestJSON)) {
			fail("manifest drift detected (run generator without --check and commit): " + *manifestPath)
		}
	} else {
		must(os.WriteFile(*manifestPath, manifestJSON, 0o644))
	}

	fmt.Printf("OK: %d fixtures processed. mode=%s\n", len(profiles), ternary(*check, "check", "write"))
}

func marshalCanonical(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	_, _ = fmt.Fprintln(os.Stderr, "FAIL:", msg)
	os.Exit(1)
}
