package recordings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RP4.1 Non-Negotiable Gates
// 1. Finality lock
// 2. Monotonicity
// 3. Small delta ignored
// 4. Final update source
// 5. State transition legality

func TestRP41_FinalityLock(t *testing.T) {
	dur := int64(100)
	meta := &RecordingMeta{
		State: StateReadyFinal,
		Facts: RecordingFacts{
			Ref:             "valid:ref",
			DurationSeconds: &dur,
			DurationFinal:   true,
		},
	}

	// 1. Try to update seconds -> fail
	_, err := ApplyDurationUpdate(meta, DurationPolicy{}, DurationUpdate{
		Seconds: 101,
		Source:  DurationContainer,
		Final:   true,
	}, time.Now())
	assert.ErrorContains(t, err, "cannot update duration in READY_FINAL")

	// 2. Try to un-finalize -> fail
	_, err = ApplyDurationUpdate(meta, DurationPolicy{}, DurationUpdate{
		Seconds: 100,
		Source:  DurationContainer,
		Final:   false,
	}, time.Now())
	assert.ErrorContains(t, err, "cannot update duration in READY_FINAL")

	// 3. Idempotent success
	ok, err := ApplyDurationUpdate(meta, DurationPolicy{}, DurationUpdate{
		Seconds: 100,
		Source:  DurationContainer,
		Final:   true,
	}, time.Now())
	assert.NoError(t, err)
	assert.False(t, ok)
}

func TestRP41_Monotonicity(t *testing.T) {
	dur := int64(100)
	meta := &RecordingMeta{
		State: StateReadyPartial,
		Facts: RecordingFacts{
			Ref:             "valid:ref",
			DurationSeconds: &dur,
		},
	}

	// Update 99 -> fail
	_, err := ApplyDurationUpdate(meta, DurationPolicy{}, DurationUpdate{
		Seconds: 99,
		Source:  DurationIndex,
	}, time.Now())
	assert.ErrorContains(t, err, "monotonicity violated")

	// Same value -> no change
	changed, err := ApplyDurationUpdate(meta, DurationPolicy{}, DurationUpdate{
		Seconds: 100,
		Source:  DurationIndex,
	}, time.Now())
	assert.NoError(t, err)
	assert.False(t, changed)
}

func TestRP41_SmallDeltaIgnored(t *testing.T) {
	dur := int64(100)
	meta := &RecordingMeta{
		State: StateReadyPartial,
		Facts: RecordingFacts{
			Ref:             "valid:ref",
			DurationSeconds: &dur,
		},
	}
	pol := DurationPolicy{MinimumDeltaSeconds: 5}

	// Delta 4 -> ignored
	changed, err := ApplyDurationUpdate(meta, pol, DurationUpdate{
		Seconds: 104,
		Source:  DurationIndex,
	}, time.Now())
	assert.NoError(t, err)
	assert.False(t, changed, "small delta should be ignored")

	// Delta 5 -> applied
	changed, err = ApplyDurationUpdate(meta, pol, DurationUpdate{
		Seconds: 105,
		Source:  DurationIndex,
	}, time.Now())
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, int64(105), *meta.Facts.DurationSeconds)
}

func TestRP41_FinalUpdateSource(t *testing.T) {
	meta := &RecordingMeta{
		State: StateReadyPartial,
		Facts: RecordingFacts{Ref: "valid:ref"},
	}

	// Final=true with METADATA -> fail
	_, err := ApplyDurationUpdate(meta, DurationPolicy{}, DurationUpdate{
		Seconds: 120,
		Source:  DurationMetadata,
		Final:   true,
	}, time.Now())
	assert.ErrorContains(t, err, "final update not allowed from source")

	// Final=true with CONTAINER -> success
	// NOTE: This will now implicitly transition to READY_FINAL to maintain invariants.
	changed, err := ApplyDurationUpdate(meta, DurationPolicy{}, DurationUpdate{
		Seconds: 120,
		Source:  DurationContainer,
		Final:   true,
	}, time.Now())
	assert.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, meta.Facts.DurationFinal)
	assert.Equal(t, StateReadyFinal, meta.State)
}

func TestRP41_StateTransitionLegality(t *testing.T) {
	// Matrix test: Check valid transitions
	tests := []struct {
		from, to RecordingState
		allowed  bool
	}{
		{StateUnknown, StateProbing, true},
		{StateUnknown, StatePreparing, true},
		{StateProbing, StatePreparing, true},
		{StateProbing, StateFailed, true},
		{StatePreparing, StateReadyPartial, true},
		{StatePreparing, StateReadyFinal, true},
		{StateReadyPartial, StateReadyFinal, true},
		{StateReadyFinal, StateFailed, true},
		{StateFailed, StatePreparing, true},
		{StateFailed, StateReadyFinal, true},
		// Illegal
		{StateReadyFinal, StateReadyPartial, false},
		{StateReadyFinal, StateUnknown, false},
		{StatePreparing, StateUnknown, false},
	}

	for _, tt := range tests {
		got := CanTransition(tt.from, tt.to)
		if got != tt.allowed {
			t.Errorf("CanTransition(%s, %s) = %v; want %v", tt.from, tt.to, got, tt.allowed)
		}
	}
}

func TestRP41_MetaValidation(t *testing.T) {
	// ReadyFinal must have DurationFinal=true
	dur := int64(100)
	meta := &RecordingMeta{
		State: StateReadyFinal,
		Facts: RecordingFacts{
			Ref:             "abc",
			DurationSeconds: &dur,
			DurationFinal:   false, // Invalid
		},
	}
	assert.Error(t, meta.Validate())

	// Fix it
	meta.Facts.DurationFinal = true
	assert.NoError(t, meta.Validate())
}
