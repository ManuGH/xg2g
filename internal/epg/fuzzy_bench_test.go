// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

import (
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkFindBestSimple benchmarks FindBest with simple inputs.
func BenchmarkFindBestSimple(b *testing.B) {
	b.Helper()
	candidates := []string{
		"Das Erste HD",
		"ZDF HD",
		"RTL HD",
		"ProSieben HD",
		"SAT.1 HD",
	}

	benchmarkFindBest(b, "Das Erste", candidates)
}

// BenchmarkFindBestLarge benchmarks FindBest with many candidates.
func BenchmarkFindBestLarge(b *testing.B) {
	b.Helper()
	candidates := make([]string, 100)
	for i := 0; i < 100; i++ {
		candidates[i] = "Channel " + string(rune('A'+i%26)) + string(rune('0'+i%10))
	}

	benchmarkFindBest(b, "Channel X5", candidates)
}

// BenchmarkFindBestUnicode benchmarks FindBest with Unicode characters.
func BenchmarkFindBestUnicode(b *testing.B) {
	b.Helper()
	candidates := []string{
		"Das Erste HD",
		"ZDF HD",
		"RTL HD",
		"ProSieben MAXX HD",
		"Sat.1 Gold HD",
		"Sixx HD",
		"ARD-alpha HD",
		"3sat HD",
		"ARTE HD",
		"BR Fernsehen Süd HD",
	}

	benchmarkFindBest(b, "ProSieben MAXX", candidates)
}

// BenchmarkBuildNameToIDMap benchmarks reading channel metadata from XMLTV.
func BenchmarkBuildNameToIDMap(b *testing.B) {
	channels := make([]Channel, 50)
	for i := 0; i < 50; i++ {
		channels[i] = Channel{
			ID:          fmt.Sprintf("channel-%d", i),
			DisplayName: []string{fmt.Sprintf("Channel %c%d", 'A'+rune(i%26), i%10)},
		}
	}

	tempDir := b.TempDir()
	xmlPath := filepath.Join(tempDir, "channels.xml")
	if err := WriteXMLTV(GenerateXMLTV(channels, nil), xmlPath); err != nil {
		b.Fatalf("WriteXMLTV: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := BuildNameToIDMap(xmlPath); err != nil {
			b.Fatalf("BuildNameToIDMap: %v", err)
		}
	}
}

func benchmarkFindBest(b *testing.B, target string, candidates []string) {
	b.Helper()
	nameToID := make(map[string]string, len(candidates))
	for i, candidate := range candidates {
		nameToID[NameKey(candidate)] = fmt.Sprintf("id-%d", i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = FindBest(target, nameToID, 2)
	}
}
