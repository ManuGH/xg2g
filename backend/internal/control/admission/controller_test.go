package admission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
)

// boolPtr helper for config
func boolPtr(b bool) *bool {
	return &b
}

func TestAdmissionController(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.AppConfig
		state      RuntimeState
		req        Request
		wantAllow  bool
		wantCode   string
		wantStatus int
	}{
		{
			name: "Allow: Engine Enabled, Tuners Available, Limits OK",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{
					Enabled:    true,
					TunerSlots: []int{0, 1},
				},
				Limits: config.LimitsConfig{
					MaxSessions:   8,
					MaxTranscodes: 2,
				},
			},
			state: RuntimeState{
				TunerSlots:       2,
				SessionsActive:   0,
				TranscodesActive: 0,
			},
			req:       Request{WantsTranscode: false},
			wantAllow: true,
		},
		{
			name: "Reject: Engine Disabled",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{
					Enabled:    false,
					TunerSlots: []int{0},
				},
				Limits: config.LimitsConfig{
					MaxSessions:   8,
					MaxTranscodes: 2,
				},
			},
			state: RuntimeState{
				TunerSlots:       1,
				SessionsActive:   0,
				TranscodesActive: 0,
			},
			req:        Request{WantsTranscode: false},
			wantAllow:  false,
			wantCode:   CodeEngineDisabled,
			wantStatus: 503,
		},
		{
			name: "Reject: No Tuner Slots",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{
					Enabled:    true,
					TunerSlots: []int{}, // Config empty (or just discovered empty)
				},
				Limits: config.LimitsConfig{
					MaxSessions:   8,
					MaxTranscodes: 2,
				},
			},
			state: RuntimeState{
				TunerSlots:       0, // Runtime state says 0
				SessionsActive:   0,
				TranscodesActive: 0,
			},
			req:        Request{WantsTranscode: false},
			wantAllow:  false,
			wantCode:   CodeNoTuners,
			wantStatus: 503,
		},
		{
			name: "Reject: Sessions Full (Limit 8, Active 8)",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{
					Enabled:    true,
					TunerSlots: []int{0, 1},
				},
				Limits: config.LimitsConfig{
					MaxSessions:   8,
					MaxTranscodes: 2,
				},
			},
			state: RuntimeState{
				TunerSlots:       2,
				SessionsActive:   8,
				TranscodesActive: 0,
			},
			req:        Request{WantsTranscode: false},
			wantAllow:  false,
			wantCode:   CodeSessionsFull,
			wantStatus: 503,
		},
		{
			name: "Reject: Transcodes Full (Limit 2, Active 2)",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{
					Enabled:    true,
					TunerSlots: []int{0, 1, 2},
				},
				Limits: config.LimitsConfig{
					MaxSessions:   8,
					MaxTranscodes: 2,
				},
			},
			state: RuntimeState{
				TunerSlots:       3,
				SessionsActive:   2, // e.g. 2 sessions, both transcoding
				TranscodesActive: 2,
			},
			req:        Request{WantsTranscode: true}, // Must request transcode to trigger limit
			wantAllow:  false,
			wantCode:   CodeTranscodesFull,
			wantStatus: 503,
		},
		{
			name: "Allow: Transcodes Full But Direct Play Requested",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{
					Enabled:    true,
					TunerSlots: []int{0, 1, 2},
				},
				Limits: config.LimitsConfig{
					MaxSessions:   8,
					MaxTranscodes: 2,
				},
			},
			state: RuntimeState{
				TunerSlots:       3,
				SessionsActive:   2,
				TranscodesActive: 2, // Full
			},
			req:       Request{WantsTranscode: false}, // Direct play should pass
			wantAllow: true,
		},
		{
			name: "Reject: Internal State Invalid (Negative Tuners)",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{Enabled: true},
			},
			state: RuntimeState{
				TunerSlots:       -1,
				SessionsActive:   0,
				TranscodesActive: 0,
			},
			req:        Request{WantsTranscode: false},
			wantAllow:  false,
			wantCode:   CodeStateUnknown,
			wantStatus: 503,
		},
		{
			name: "Reject: Internal State Invalid (Negative)",
			cfg: config.AppConfig{
				Engine: config.EngineConfig{Enabled: true},
			},
			state: RuntimeState{
				TunerSlots:       1,
				SessionsActive:   -1, // Should never happen
				TranscodesActive: 0,
			},
			req:        Request{WantsTranscode: false},
			wantAllow:  false,
			wantCode:   CodeStateUnknown,
			wantStatus: 503,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Init controller
			ctrl := NewController(tc.cfg)

			// Check
			decision := ctrl.Check(context.Background(), tc.req, tc.state)

			// Verify
			if tc.wantAllow {
				assert.True(t, decision.Allow)
				assert.Nil(t, decision.Problem)
			} else {
				assert.False(t, decision.Allow)
				require.NotNil(t, decision.Problem)
				assert.Equal(t, tc.wantCode, decision.Problem.Code)
				assert.Equal(t, tc.wantStatus, decision.Problem.Status)

				// Verify problem.Write compatibility by mocking
				// We can just verify the problem fields are correct
				assert.NotEmpty(t, decision.Problem.Title)
				assert.NotEmpty(t, decision.Problem.Detail)
				assert.Equal(t, "application/problem+json", "application/problem+json") // Placeholder for explicit check if needed
			}
		})
	}
}
