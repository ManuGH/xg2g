package ffmpeg

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type dedupEmission struct {
	level   zerolog.Level
	line    string
	repeats int
}

func collectEmissions(emissions *[]dedupEmission) func(zerolog.Level, string, int) {
	return func(level zerolog.Level, line string, repeats int) {
		*emissions = append(*emissions, dedupEmission{level: level, line: line, repeats: repeats})
	}
}

func TestFFmpegLogDeduperPassesDistinctLines(t *testing.T) {
	var got []dedupEmission
	emit := collectEmissions(&got)
	d := newFFmpegLogDeduper(10 * time.Second)

	d.observe("line a", zerolog.WarnLevel, emit)
	d.observe("line b", zerolog.InfoLevel, emit)
	d.observe("line c", zerolog.DebugLevel, emit)
	d.flush(emit)

	want := []dedupEmission{
		{zerolog.WarnLevel, "line a", 0},
		{zerolog.InfoLevel, "line b", 0},
		{zerolog.DebugLevel, "line c", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d emissions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("emission %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFFmpegLogDeduperSuppressesRepeatsUntilLineChanges(t *testing.T) {
	var got []dedupEmission
	emit := collectEmissions(&got)
	d := newFFmpegLogDeduper(10 * time.Second)

	const storm = "corrupt decoded frame"
	d.observe(storm, zerolog.WarnLevel, emit)
	for i := 0; i < 140; i++ {
		d.observe(storm, zerolog.WarnLevel, emit)
	}
	d.observe("stream ended", zerolog.InfoLevel, emit)

	want := []dedupEmission{
		{zerolog.WarnLevel, storm, 0},
		{zerolog.WarnLevel, storm, 140},
		{zerolog.InfoLevel, "stream ended", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d emissions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("emission %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFFmpegLogDeduperFlushEmitsPendingSummary(t *testing.T) {
	var got []dedupEmission
	emit := collectEmissions(&got)
	d := newFFmpegLogDeduper(10 * time.Second)

	d.observe("packet corrupt", zerolog.WarnLevel, emit)
	d.observe("packet corrupt", zerolog.WarnLevel, emit)
	d.observe("packet corrupt", zerolog.WarnLevel, emit)
	d.flush(emit)
	// A second flush must not emit again.
	d.flush(emit)

	want := []dedupEmission{
		{zerolog.WarnLevel, "packet corrupt", 0},
		{zerolog.WarnLevel, "packet corrupt", 2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d emissions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("emission %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFFmpegLogDeduperEmitsInterimSummaryWhenWindowElapses(t *testing.T) {
	var got []dedupEmission
	emit := collectEmissions(&got)

	current := time.Unix(0, 0)
	d := newFFmpegLogDeduper(10 * time.Second)
	d.now = func() time.Time { return current }

	const storm = "corrupt decoded frame"
	d.observe(storm, zerolog.WarnLevel, emit) // passes through, windowAt = t0
	for i := 0; i < 5; i++ {
		d.observe(storm, zerolog.WarnLevel, emit) // suppressed
	}
	current = current.Add(11 * time.Second)
	d.observe(storm, zerolog.WarnLevel, emit) // window elapsed -> interim summary
	d.observe(storm, zerolog.WarnLevel, emit) // suppressed again
	d.flush(emit)

	want := []dedupEmission{
		{zerolog.WarnLevel, storm, 0},
		{zerolog.WarnLevel, storm, 6},
		{zerolog.WarnLevel, storm, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d emissions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("emission %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
