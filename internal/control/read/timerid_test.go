// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package read

import (
	"testing"
)

func TestTimerID(t *testing.T) {
	tests := []struct {
		name       string
		serviceRef string
		begin      int64
		end        int64
		wantErr    bool
	}{
		{
			name:       "Simple Valid",
			serviceRef: "1:0:19:2B66:3F3:1:C00000:0:0:0:",
			begin:      1600000000,
			end:        1600003600,
			wantErr:    false,
		},
		{
			name:       "Complex ServiceRef",
			serviceRef: "1:0:1:445C:453:1:C00000:0:0:0:http%3a//example.com/stream.m3u8",
			begin:      1700000000,
			end:        1700007200,
			wantErr:    false,
		},
		{
			name:       "Invalid Time Order",
			serviceRef: "foo",
			begin:      1600003600,
			end:        1600000000,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			id := MakeTimerID(tt.serviceRef, tt.begin, tt.end)
			if id == "" {
				t.Error("MakeTimerID returned empty string")
			}

			// Decode
			gotRef, gotBegin, gotEnd, err := ParseTimerID(id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTimerID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if gotRef != tt.serviceRef {
					t.Errorf("ParseTimerID() serviceRef = %v, want %v", gotRef, tt.serviceRef)
				}
				if gotBegin != tt.begin {
					t.Errorf("ParseTimerID() begin = %v, want %v", gotBegin, tt.begin)
				}
				if gotEnd != tt.end {
					t.Errorf("ParseTimerID() end = %v, want %v", gotEnd, tt.end)
				}
			}
		})
	}
}

func TestParseTimerID_InvalidInputs(t *testing.T) {
	invalidIDs := []string{
		"",
		"notbase64",
		"MQ==|",      // Valid base64 but not our format
		"MSwyLDM=",   // "1,2,3"
		"MXxmb298Mw", // "1|foo|3" (middle not int)
	}

	for _, id := range invalidIDs {
		_, _, _, err := ParseTimerID(id)
		if err == nil {
			t.Errorf("ParseTimerID(%q) expected error, got nil", id)
		}
	}
}
