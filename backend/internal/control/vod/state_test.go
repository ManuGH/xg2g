package vod

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVOD_StateMachine validates all legal and illegal state transitions (Gate M1).
func TestVOD_StateMachine(t *testing.T) {
	tests := []struct {
		name      string
		from      State
		event     TransitionEvent
		wantState State
		wantValid bool
	}{
		// Legal transitions
		{"Idle->Building on Start", StateIdle, EventStart, StateBuilding, true},
		{"Building->Finalizing on BuildComplete", StateBuilding, EventBuildComplete, StateFinalizing, true},
		{"Building->Failed on Fail", StateBuilding, EventFail, StateFailed, true},
		{"Building->Canceled on Cancel", StateBuilding, EventCancel, StateCanceled, true},
		{"Finalizing->Succeeded on PublishComplete", StateFinalizing, EventPublishComplete, StateSucceeded, true},
		{"Finalizing->Failed on Fail", StateFinalizing, EventFail, StateFailed, true},

		// Illegal transitions (should reject)
		{"Idle->Finalizing invalid", StateIdle, EventBuildComplete, StateIdle, false},
		{"Idle->Failed invalid", StateIdle, EventFail, StateIdle, false},
		{"Building->Succeeded invalid", StateBuilding, EventPublishComplete, StateBuilding, false},
		{"Succeeded->Building invalid (terminal)", StateSucceeded, EventStart, StateSucceeded, false},
		{"Failed->Building invalid (terminal)", StateFailed, EventStart, StateFailed, false},
		{"Canceled->Building invalid (terminal)", StateCanceled, EventStart, StateCanceled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canTransition := CanTransition(tt.from, tt.event)
			assert.Equal(t, tt.wantValid, canTransition, "CanTransition mismatch")

			newState := Transition(tt.from, tt.event)
			assert.Equal(t, tt.wantState, newState, "Transition result mismatch")
		})
	}
}

func TestState_IsTerminal(t *testing.T) {
	assert.False(t, StateIdle.IsTerminal())
	assert.False(t, StateBuilding.IsTerminal())
	assert.False(t, StateFinalizing.IsTerminal())
	assert.True(t, StateSucceeded.IsTerminal())
	assert.True(t, StateFailed.IsTerminal())
	assert.True(t, StateCanceled.IsTerminal())
}

func TestState_String(t *testing.T) {
	assert.Equal(t, "Idle", StateIdle.String())
	assert.Equal(t, "Building", StateBuilding.String())
	assert.Equal(t, "Finalizing", StateFinalizing.String())
	assert.Equal(t, "Succeeded", StateSucceeded.String())
	assert.Equal(t, "Failed", StateFailed.String())
	assert.Equal(t, "Canceled", StateCanceled.String())
}
