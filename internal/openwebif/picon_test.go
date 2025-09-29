// SPDX-License-Identifier: MIT
package openwebif

import "testing"

func TestPiconURL(t *testing.T) {
	tests := []struct {
		name    string
		owiBase string
		sref    string
		want    string
	}{
		{
			name:    "basic_url",
			owiBase: "http://receiver.local",
			sref:    "1:0:1:1234:5678:9ABC:DEF0:0:0:0:",
			want:    "http://receiver.local/picon/1:0:1:1234:5678:9ABC:DEF0:0:0:0:.png",
		},
		{
			name:    "base_with_trailing_slash",
			owiBase: "http://receiver.local/",
			sref:    "1:0:1:1234:5678:9ABC:DEF0:0:0:0:",
			want:    "http://receiver.local/picon/1:0:1:1234:5678:9ABC:DEF0:0:0:0:.png",
		},
		{
			name:    "sref_with_special_chars",
			owiBase: "https://receiver.local:8080",
			sref:    "1:0:1:ABC#:DEF%:9ABC:DEF0:0:0:0:",
			want:    "https://receiver.local:8080/picon/1:0:1:ABC%23:DEF%25:9ABC:DEF0:0:0:0:.png",
		},
		{
			name:    "empty_sref",
			owiBase: "http://receiver.local",
			sref:    "",
			want:    "http://receiver.local/picon/.png",
		},
		{
			name:    "ipv6_address",
			owiBase: "http://[::1]:8080",
			sref:    "1:0:1:1234:5678:9ABC:DEF0:0:0:0:",
			want:    "http://[::1]:8080/picon/1:0:1:1234:5678:9ABC:DEF0:0:0:0:.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PiconURL(tt.owiBase, tt.sref)
			if got != tt.want {
				t.Errorf("PiconURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
