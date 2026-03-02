package read

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
)

func TestMapOpenWebIFTimerState_TruthTable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0) // fixed reference

	tests := []struct {
		name string
		in   openwebif.Timer
		want TimerState
	}{
		{
			name: "disabled_wins_even_if_in_progress",
			in: openwebif.Timer{
				Disabled: 1,
				Begin:    now.Add(-10 * time.Minute).Unix(),
				End:      now.Add(10 * time.Minute).Unix(),
				State:    0,
			},
			want: TimerStateDisabled,
		},
		{
			name: "in_progress_maps_to_recording",
			in: openwebif.Timer{
				Disabled: 0,
				Begin:    now.Add(-1 * time.Minute).Unix(),
				End:      now.Add(5 * time.Minute).Unix(),
				State:    123, // unknown raw state, but time window determines recording
			},
			want: TimerStateRecording,
		},
		{
			name: "future_maps_to_scheduled",
			in: openwebif.Timer{
				Disabled: 0,
				Begin:    now.Add(5 * time.Minute).Unix(),
				End:      now.Add(10 * time.Minute).Unix(),
				State:    999,
			},
			want: TimerStateScheduled,
		},
		{
			name: "past_unknown_raw_maps_to_unknown",
			in: openwebif.Timer{
				Disabled: 0,
				Begin:    now.Add(-10 * time.Minute).Unix(),
				End:      now.Add(-5 * time.Minute).Unix(),
				State:    999,
			},
			want: TimerStateUnknown,
		},
		{
			name: "missing_times_fail_closed_to_unknown",
			in: openwebif.Timer{
				Disabled: 0,
				Begin:    0,
				End:      0,
				State:    999,
			},
			want: TimerStateUnknown,
		},
		{
			name: "end_before_begin_still_future_maps_to_scheduled",
			in: openwebif.Timer{
				Disabled: 0,
				Begin:    now.Add(10 * time.Minute).Unix(),
				End:      now.Add(5 * time.Minute).Unix(),
				State:    999,
			},
			want: TimerStateScheduled, // because now < Begin; begin is authoritative for scheduling
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapOpenWebIFTimerState(tt.in, now)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMapOpenWebIFTimerState_KnownRawStates_AreHonoredForPast(t *testing.T) {
	// Temporarily set known mapping for this test.
	old := KnownTimerStates
	t.Cleanup(func() { KnownTimerStates = old })

	KnownTimerStates = map[int]RawStateMeaning{
		7: rawCompleted,
		8: rawFailed,
		9: rawCanceled,
	}

	now := time.Unix(1_700_000_000, 0)
	pastBegin := now.Add(-10 * time.Minute).Unix()
	pastEnd := now.Add(-5 * time.Minute).Unix()

	tests := []struct {
		name string
		raw  int
		want TimerState
	}{
		{"completed", 7, TimerStateCompleted},
		{"failed", 8, TimerStateFailed},
		{"canceled", 9, TimerStateCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := openwebif.Timer{
				Disabled: 0,
				Begin:    pastBegin,
				End:      pastEnd,
				State:    tt.raw,
			}
			got := MapOpenWebIFTimerState(in, now)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMapOpenWebIFTimerToDTO_SetsTimerIDAndState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	in := openwebif.Timer{
		ServiceRef:  "1:0:19:132F:3EF:1:C00000:0:0:0:",
		ServiceName: "ORF1 HD",
		Name:        "Test",
		Description: "Desc",
		Disabled:    0,
		Begin:       now.Add(5 * time.Minute).Unix(),
		End:         now.Add(10 * time.Minute).Unix(),
		State:       999,
	}

	dto := MapOpenWebIFTimerToDTO(in, now)
	assert.NotEmpty(t, dto.TimerID)
	assert.Equal(t, TimerStateScheduled, dto.State)
	assert.Equal(t, in.ServiceRef, dto.ServiceRef)
	assert.Equal(t, in.Begin, dto.Begin)
	assert.Equal(t, in.End, dto.End)
}
