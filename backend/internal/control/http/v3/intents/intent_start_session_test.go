// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package intents

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/require"
)

func TestBuildStartSession_DVRWindowOverrides(t *testing.T) {
	svc := NewService(newMockDeps())
	res := startProfileResolution{
		profileSpec: model.ProfileSpec{
			Name: "universal",
		},
	}

	tests := []struct {
		name    string
		params  map[string]string
		wantSec int
		wantTTL time.Duration
	}{
		{
			name:    "default zero",
			params:  map[string]string{},
			wantSec: 0,
			wantTTL: 30 * time.Second,
		},
		{
			name:    "explicit dvr_window_sec",
			params:  map[string]string{"dvr_window_sec": "3600"},
			wantSec: 3600,
			wantTTL: time.Hour + 30*time.Second,
		},
		{
			name:    "dvr true enables default 2h window",
			params:  map[string]string{"dvr": "true"},
			wantSec: 7200,
			wantTTL: 2*time.Hour + 30*time.Second,
		},
		{
			name:    "dvr false sets zero window",
			params:  map[string]string{"dvr": "false"},
			wantSec: 0,
			wantTTL: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := Intent{
				SessionID:  "test-session",
				ServiceRef: "1:0:1:1234:56:1:C00000:0:0:0:",
				Params:     tt.params,
			}
			before := time.Now().Unix()
			rec := svc.buildStartSession(intent, res)
			after := time.Now().Unix()
			if rec.Profile.DVRWindowSec != tt.wantSec {
				t.Errorf("DVRWindowSec = %d, want %d", rec.Profile.DVRWindowSec, tt.wantSec)
			}
			require.GreaterOrEqual(t, rec.LeaseExpiresAtUnix, before+int64(tt.wantTTL/time.Second))
			require.LessOrEqual(t, rec.LeaseExpiresAtUnix, after+int64(tt.wantTTL/time.Second))
		})
	}
}
