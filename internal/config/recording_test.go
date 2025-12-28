// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"
)

func TestParseRecordingMappings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []RecordingPathMapping
	}{
		{
			name:  "empty string returns defaults",
			input: "",
			expected: []RecordingPathMapping{
				{ReceiverRoot: "/default", LocalRoot: "/mount"},
			},
		},
		{
			name:  "single mapping",
			input: "/media/net/movie=/media/nfs-recordings",
			expected: []RecordingPathMapping{
				{ReceiverRoot: "/media/net/movie", LocalRoot: "/media/nfs-recordings"},
			},
		},
		{
			name:  "multiple mappings",
			input: "/media/net/movie=/media/nfs-recordings;/media/hdd/movie=/mnt/hdd",
			expected: []RecordingPathMapping{
				{ReceiverRoot: "/media/net/movie", LocalRoot: "/media/nfs-recordings"},
				{ReceiverRoot: "/media/hdd/movie", LocalRoot: "/mnt/hdd"},
			},
		},
		{
			name:  "whitespace handling",
			input: " /media/net/movie = /media/nfs-recordings ; /media/hdd/movie = /mnt/hdd ",
			expected: []RecordingPathMapping{
				{ReceiverRoot: "/media/net/movie", LocalRoot: "/media/nfs-recordings"},
				{ReceiverRoot: "/media/hdd/movie", LocalRoot: "/mnt/hdd"},
			},
		},
		{
			name:     "invalid format returns defaults",
			input:    "invalid",
			expected: []RecordingPathMapping{{ReceiverRoot: "/default", LocalRoot: "/mount"}},
		},
		{
			name:     "empty entries skipped",
			input:    ";;",
			expected: []RecordingPathMapping{{ReceiverRoot: "/default", LocalRoot: "/mount"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaults := []RecordingPathMapping{
				{ReceiverRoot: "/default", LocalRoot: "/mount"},
			}
			result := parseRecordingMappings(tt.input, defaults)

			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d mappings, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				if result[i].ReceiverRoot != expected.ReceiverRoot {
					t.Errorf("mapping[%d].ReceiverRoot = %q, want %q", i, result[i].ReceiverRoot, expected.ReceiverRoot)
				}
				if result[i].LocalRoot != expected.LocalRoot {
					t.Errorf("mapping[%d].LocalRoot = %q, want %q", i, result[i].LocalRoot, expected.LocalRoot)
				}
			}
		})
	}
}
