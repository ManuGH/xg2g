// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

// TestValidateConfig_Comprehensive tests the complete config validation with various scenarios
func TestValidateConfig_Comprehensive(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.AppConfig
		wantErr bool
		errSub  string // substring expected in error message
	}{
		// Happy paths
		{
			name: "valid minimal config",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost:8080",
					StreamPort: 8001,
				},
				DataDir: t.TempDir(),
			},
			wantErr: false,
		},
		{
			name: "valid full config with picons",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "https://example.com:80",
					StreamPort: 17999,
				},
				DataDir:   t.TempDir(),
				PiconBase: "https://example.com/picons",
				XMLTVPath: "/path/to/xmltv",
				Bouquet:   "Premium,Favourites",
			},
			wantErr: false,
		},
		{
			name: "valid config with empty picon base",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://192.168.1.1",
					StreamPort: 8001,
				},
				DataDir:   t.TempDir(),
				PiconBase: "", // empty is valid
			},
			wantErr: false,
		},

		// OWIBase validation errors
		{
			name: "missing OWIBase",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "",
					StreamPort: 8001,
				},
				DataDir: t.TempDir(),
			},
			wantErr: true,
			errSub:  "Enigma2.BaseURL",
		},
		{
			name: "invalid OWIBase scheme (ftp)",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "ftp://example.com",
					StreamPort: 8001,
				},
				DataDir: t.TempDir(),
			},
			wantErr: true,
			errSub:  "Enigma2.BaseURL",
		},
		{
			name: "invalid OWIBase format",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "not-a-url",
					StreamPort: 8001,
				},
				DataDir: t.TempDir(),
			},
			wantErr: true,
			errSub:  "Enigma2.BaseURL",
		},
		{
			name: "OWIBase missing host",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http:///path",
					StreamPort: 8001,
				},
				DataDir: t.TempDir(),
			},
			wantErr: true,
			errSub:  "Enigma2.BaseURL",
		},

		// StreamPort validation errors
		{
			name: "invalid port zero",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: 0,
				},
				DataDir: t.TempDir(),
			},
			wantErr: true,
			errSub:  "Enigma2.StreamPort",
		},
		{
			name: "invalid port negative",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: -1,
				},
				DataDir: t.TempDir(),
			},
			wantErr: true,
			errSub:  "Enigma2.StreamPort",
		},
		{
			name: "invalid port too high",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: 99999,
				},
				DataDir: t.TempDir(),
			},
			wantErr: true,
			errSub:  "StreamPort",
		},

		// DataDir validation errors
		{
			name: "missing DataDir",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: 8001,
				},
				DataDir: "",
			},
			wantErr: true,
			errSub:  "DataDir",
		},

		// PiconBase validation errors (when set)
		{
			name: "invalid PiconBase scheme",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: 8001,
				},
				DataDir:   t.TempDir(),
				PiconBase: "ftp://example.com/picons",
			},
			wantErr: true,
			errSub:  "PiconBase",
		},
		{
			name: "invalid PiconBase format",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: 8001,
				},
				DataDir:   t.TempDir(),
				PiconBase: "not-a-url",
			},
			wantErr: true,
			errSub:  "PiconBase",
		},
		{
			name: "PiconBase missing host",
			cfg: config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: 8001,
				},
				DataDir:   t.TempDir(),
				PiconBase: "http:///path",
			},
			wantErr: true,
			errSub:  "PiconBase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
					t.Fatalf("expected error containing %q, got %v", tt.errSub, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestValidateConfig_PiconBase tests PiconBase validation specifically
// This test is kept for historical reasons and focused PiconBase testing
func TestValidateConfig_PiconBase(t *testing.T) {
	tests := []struct {
		name      string
		piconBase string
		wantErr   bool
	}{
		{
			name:      "empty picon base is valid",
			piconBase: "",
			wantErr:   false,
		},
		{
			name:      "valid http url",
			piconBase: "http://example.com/picons",
			wantErr:   false,
		},
		{
			name:      "valid https url",
			piconBase: "https://example.com/picons",
			wantErr:   false,
		},
		{
			name:      "invalid scheme",
			piconBase: "ftp://example.com/picons",
			wantErr:   true,
		},
		{
			name:      "invalid url",
			piconBase: "not-a-url",
			wantErr:   true,
		},
		{
			name:      "missing host",
			piconBase: "http:///path",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.AppConfig{
				Enigma2: config.Enigma2Settings{
					BaseURL:    "http://localhost",
					StreamPort: 8001,
				},
				DataDir:   t.TempDir(),
				PiconBase: tt.piconBase,
			}

			err := validateConfig(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
