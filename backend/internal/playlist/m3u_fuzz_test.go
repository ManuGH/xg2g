// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
//go:build go1.18

package playlist

import (
	"bytes"
	"testing"
)

// FuzzWriteM3U fuzzes the M3U playlist generator to ensure it doesn't panic
// on various input combinations and produces valid output.
func FuzzWriteM3U(f *testing.F) {
	// Seed corpus with valid examples
	f.Add("Channel 1", "ch1", 1, "http://logo.png", "Group1", "http://stream1")
	f.Add("Test & <Special>", "test-id", 100, "", "Default", "http://example.com/stream")
	f.Add("", "", 0, "", "", "")
	f.Add("Unicode Тест", "unicode-1", 42, "http://example.com/logo.png", "Интер", "rtsp://stream")

	f.Fuzz(func(t *testing.T, name, tvgID string, tvgChNo int, tvgLogo, group, url string) {
		// Normalize tvgChNo to valid range
		if tvgChNo < 0 {
			tvgChNo = 0
		}
		if tvgChNo > 9999 {
			tvgChNo = 9999
		}

		items := []Item{
			{
				Name:    name,
				TvgID:   tvgID,
				TvgChNo: tvgChNo,
				TvgLogo: tvgLogo,
				Group:   group,
				URL:     url,
			},
		}

		var buf bytes.Buffer
		err := WriteM3U(&buf, items, "", "")
		if err != nil {
			t.Fatalf("WriteM3U failed: %v", err)
		}

		output := buf.String()

		// Basic validation: must start with #EXTM3U
		if len(output) == 0 {
			t.Error("output is empty")
		}
		if len(output) > 0 && output[0:7] != "#EXTM3U" {
			t.Errorf("output doesn't start with #EXTM3U: %s", output[:minInt(50, len(output))])
		}

		// Must contain #EXTINF if items are present
		if len(items) > 0 && !bytes.Contains(buf.Bytes(), []byte("#EXTINF")) {
			t.Error("output missing #EXTINF for non-empty playlist")
		}
	})
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
