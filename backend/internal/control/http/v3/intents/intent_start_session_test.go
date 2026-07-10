// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package intents

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestBuildStartSession_DVRWindowOverrides(t *testing.T) {
	svc := NewService(newMockDeps())
	res := startProfileResolution{
		profileSpec: model.ProfileSpec{
			Name: "universal",
		},
	}

	tests := []struct {
		name     string
		params   map[string]string
		wantSec  int
	}{
		{
			name:    "default zero",
			params:  map[string]string{},
			wantSec: 0,
		},
		{
			name:    "explicit dvr_window_sec",
			params:  map[string]string{"dvr_window_sec": "3600"},
			wantSec: 3600,
		},
		{
			name:    "dvr true enables default 2h window",
			params:  map[string]string{"dvr": "true"},
			wantSec: 7200,
		},
		{
			name:    "dvr false sets zero window",
			params:  map[string]string{"dvr": "false"},
			wantSec: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := Intent{
				SessionID:  "test-session",
				ServiceRef: "1:0:1:1234:56:1:C00000:0:0:0:",
				Params:     tt.params,
			}
			rec := svc.buildStartSession(intent, res)
			if rec.Profile.DVRWindowSec != tt.wantSec {
				t.Errorf("DVRWindowSec = %d, want %d", rec.Profile.DVRWindowSec, tt.wantSec)
			}
		})
	}
}
