package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidation_MissingConfigFails tests that missing required config fails early
func TestValidation_MissingConfigFails(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	bus := NewStubBus()

	testCases := []struct {
		name        string
		setupOrch   func() *Orchestrator
		expectedErr string
	}{
		{
			name: "missing StartConcurrency",
			setupOrch: func() *Orchestrator {
				return &Orchestrator{
					Store:               st,
					Bus:                 bus,
					Admission:           admission.NewResourceMonitor(10, 10, 0),
					LeaseTTL:            30 * time.Second,
					HeartbeatEvery:      10 * time.Second,
					Owner:               "test",
					PipelineStopTimeout: 5 * time.Second,
					StartConcurrency:    0, // INVALID
					StopConcurrency:     5,
					Sweeper: SweeperConfig{
						Interval:         5 * time.Minute,
						SessionRetention: 24 * time.Hour,
					},
				}
			},
			expectedErr: "StartConcurrency must be > 0",
		},
		{
			name: "missing LeaseTTL",
			setupOrch: func() *Orchestrator {
				return &Orchestrator{
					Store:               st,
					Bus:                 bus,
					Admission:           admission.NewResourceMonitor(10, 10, 0),
					LeaseTTL:            0, // INVALID
					HeartbeatEvery:      10 * time.Second,
					Owner:               "test",
					PipelineStopTimeout: 5 * time.Second,
					StartConcurrency:    5,
					StopConcurrency:     5,
					Sweeper: SweeperConfig{
						Interval:         5 * time.Minute,
						SessionRetention: 24 * time.Hour,
					},
				}
			},
			expectedErr: "LeaseTTL must be > 0",
		},
		{
			name: "missing Owner",
			setupOrch: func() *Orchestrator {
				return &Orchestrator{
					Store:               st,
					Bus:                 bus,
					Admission:           admission.NewResourceMonitor(10, 10, 0),
					LeaseTTL:            30 * time.Second,
					HeartbeatEvery:      10 * time.Second,
					Owner:               "", // INVALID
					PipelineStopTimeout: 5 * time.Second,
					StartConcurrency:    5,
					StopConcurrency:     5,
					Sweeper: SweeperConfig{
						Interval:         5 * time.Minute,
						SessionRetention: 24 * time.Hour,
					},
				}
			},
			expectedErr: "owner must be set",
		},
		{
			name: "missing Sweeper.Interval",
			setupOrch: func() *Orchestrator {
				return &Orchestrator{
					Store:               st,
					Bus:                 bus,
					Admission:           admission.NewResourceMonitor(10, 10, 0),
					LeaseTTL:            30 * time.Second,
					HeartbeatEvery:      10 * time.Second,
					Owner:               "test",
					PipelineStopTimeout: 5 * time.Second,
					StartConcurrency:    5,
					StopConcurrency:     5,
					Sweeper: SweeperConfig{
						Interval:         0, // INVALID
						SessionRetention: 24 * time.Hour,
					},
				}
			},
			expectedErr: "Sweeper.Interval must be > 0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			orch := tc.setupOrch()
			orch.LeaseKeyFunc = func(e model.StartSessionEvent) string {
				return model.LeaseKeyService(e.ServiceRef)
			}

			err := orch.Run(ctx)
			require.Error(t, err, "Expected validation to fail")
			assert.Contains(t, err.Error(), tc.expectedErr)
		})
	}
}
