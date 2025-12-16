// SPDX-License-Identifier: MIT
package epg

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

func TestProgrammesFromEPG(t *testing.T) {
	now := time.Now()
	events := []openwebif.EPGEvent{
		{
			ID:          123,
			Title:       "Test Programme",
			Description: "Short desc",
			LongDesc:    "Long description",
			Begin:       now.Unix(),
			Duration:    3600, // 1 hour
			SRef:        "1:0:1:123::",
		},
		{
			ID:       124,
			Title:    "", // Invalid - should be skipped
			Begin:    now.Unix(),
			Duration: 1800,
		},
		{
			ID:          125,
			Title:       "No Duration",
			Description: "Test",
			Begin:       now.Unix() + 3600,
			Duration:    0, // Will get default 30min
			SRef:        "1:0:1:125::",
		},
	}

	programmes := ProgrammesFromEPG(events, "test.channel")

	if len(programmes) != 2 {
		t.Fatalf("expected 2 programmes, got %d", len(programmes))
	}

	prog1 := programmes[0]
	if prog1.Title.Text != "Test Programme" {
		t.Errorf("expected title 'Test Programme', got %q", prog1.Title.Text)
	}

	if prog1.Channel != "test.channel" {
		t.Errorf("expected channel 'test.channel', got %q", prog1.Channel)
	}

	if prog1.Desc != "Long description" {
		t.Errorf("expected long description, got %q", prog1.Desc)
	}

	// Test programme with default duration
	prog2 := programmes[1]
	if prog2.Title.Text != "No Duration" {
		t.Errorf("expected title 'No Duration', got %q", prog2.Title.Text)
	}
}

func TestFormatXMLTVTime(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 20, 30, 0, 0, time.UTC)
	expected := "20240115203000 +0000"

	result := formatXMLTVTime(testTime)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestBuildDescription(t *testing.T) {
	tests := []struct {
		name     string
		event    openwebif.EPGEvent
		expected string
	}{
		{
			name: "long_desc_different",
			event: openwebif.EPGEvent{
				Description: "Short",
				LongDesc:    "Long description",
			},
			expected: "Long description",
		},
		{
			name: "long_desc_same_as_short",
			event: openwebif.EPGEvent{
				Description: "Same desc",
				LongDesc:    "Same desc",
			},
			expected: "Same desc",
		},
		{
			name: "only_short_desc",
			event: openwebif.EPGEvent{
				Description: "Short only",
				LongDesc:    "",
			},
			expected: "Short only",
		},
		{
			name: "no_description",
			event: openwebif.EPGEvent{
				Description: "",
				LongDesc:    "",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDescription(tt.event)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
