package recordings

import (
	"testing"
)

func TestValidateRecordingRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{"ValidDVR", "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie.ts", false},
		{"ValidDVR_Nested", "1:0:1:0:0:0:0:0:0:0:/media/hdd/shows/movie.ts", false},
		{"LiveRef_Fails_DVR_Validation", "1:0:19:283D:3FB:1:C00000:0:0:0:", true},
		{"PathTraversal", "1:0:1:0:0:0:0:0:0:0:/media/hdd/../movie.ts", true},
		{"MissingColons", "media/hdd/movie.ts", true},
		{"Empty", "", true},
		{"ControlChars", "1:0:1:0:0:\x00:/media/hdd/movie.ts", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecordingRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRecordingRef() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateLiveRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{"ValidLive", "1:0:19:283D:3FB:1:C00000:0:0:0:", false},
		{"DVRContent_FailsLiveValidation", "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie.ts", true},
		{"ValidLive_WithUrlChars", "1:0:1:C35C:271A:F001:FFFF0000:0:0:0:", false},
		{"MissingColons", "justastring", true},
		{"Empty", "", true},
		{"PathTraversalAttack", "1:0:19:283D:3FB:1:C00000:0:0:0:/../etc/passwd", true},
		{"ControlChars", "1:0:1:0:0:\x00:", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLiveRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLiveRef() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
