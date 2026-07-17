package telemetry

import (
	"testing"
	"time"
)

func TestStartupTracerLifecycle(t *testing.T) {
	sessionID := "test-session-123"

	// Create tracer
	tracer := NewStartupTracer(sessionID)
	if tracer == nil {
		t.Fatal("expected tracer, got nil")
	}

	// Should exist in registry
	got := GetStartupTracer(sessionID)
	if got == nil {
		t.Fatal("expected to retrieve tracer, got nil")
	}

	// Test marking (should not panic/race)
	go func() {
		tracer.MarkOnce(MilestoneT1, "test_phase")
		tracer.UpdateMetadata("client", "container", "video", "input")
	}()

	go func() {
		tracer.MarkOnce(MilestoneT2, "test_phase_2")
		tracer.Summary()
	}()

	time.Sleep(50 * time.Millisecond)

	// Test removal
	RemoveStartupTracer(sessionID)

	// Should be noop tracer now
	removed := GetStartupTracer(sessionID)
	if _, isNoop := removed.(*noopTracer); !isNoop {
		t.Errorf("expected noopTracer after removal, got %T", removed)
	}

	// Noop tracer methods shouldn't panic
	removed.MarkOnce(MilestoneT1, "test")
	removed.Summary()
}
