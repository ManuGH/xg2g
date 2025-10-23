package epg

import (
	"testing"
)

// BenchmarkFindBestMatchSimple benchmarks FindBestMatch with simple inputs
func BenchmarkFindBestMatchSimple(b *testing.B) {
	candidates := []string{
		"Das Erste HD",
		"ZDF HD",
		"RTL HD",
		"ProSieben HD",
		"SAT.1 HD",
	}

	target := "Das Erste"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = FindBestMatch(target, candidates)
	}
}

// BenchmarkFindBestMatchLarge benchmarks FindBestMatch with many candidates
func BenchmarkFindBestMatchLarge(b *testing.B) {
	// Simulate real-world scenario with 100+ channels
	candidates := make([]string, 100)
	for i := 0; i < 100; i++ {
		candidates[i] = "Channel " + string(rune('A'+i%26)) + string(rune('0'+i%10))
	}

	target := "Channel X5"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = FindBestMatch(target, candidates)
	}
}

// BenchmarkFindBestMatchUnicode benchmarks with Unicode characters
func BenchmarkFindBestMatchUnicode(b *testing.B) {
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

	target := "ProSieben MAXX"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = FindBestMatch(target, candidates)
	}
}

// BenchmarkBuildNameToIDMap benchmarks map building
func BenchmarkBuildNameToIDMap(b *testing.B) {
	channels := make([]Channel, 50)
	for i := 0; i < 50; i++ {
		channels[i] = Channel{
			ID:          string(rune('A' + i%26)),
			DisplayName: []DisplayName{{Value: "Channel " + string(rune('A'+i%26))}},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = BuildNameToIDMap(channels)
	}
}
