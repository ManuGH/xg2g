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
			name:    "sd_service",
			owiBase: "http://receiver.local",
			sref:    "1:0:1:1234:5678:9ABC:DEF0:0:0:0:",
			want:    "http://receiver.local/picon/1_0_1_1234_5678_9ABC_DEF0_0_0_0.png",
		},
		{
			name:    "hd_service_type_19_normalized_to_sd",
			owiBase: "http://receiver.local",
			sref:    "1:0:19:132F:3EF:1:C00000:0:0:0:",
			want:    "http://receiver.local/picon/1_0_1_132F_3EF_1_C00000_0_0_0.png",
		},
		{
			name:    "base_with_trailing_slash",
			owiBase: "http://receiver.local/",
			sref:    "1:0:1:1234:5678:9ABC:DEF0:0:0:0:",
			want:    "http://receiver.local/picon/1_0_1_1234_5678_9ABC_DEF0_0_0_0.png",
		},
		{
			name:    "hd_service_type_1f_hevc",
			owiBase: "http://receiver.local",
			sref:    "1:0:1F:1234:5678:9ABC:DEF0:0:0:0:",
			want:    "http://receiver.local/picon/1_0_1_1234_5678_9ABC_DEF0_0_0_0.png",
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
			want:    "http://[::1]:8080/picon/1_0_1_1234_5678_9ABC_DEF0_0_0_0.png",
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
