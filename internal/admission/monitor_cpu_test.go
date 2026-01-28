package admission

import (
	"context"
	"testing"
	"time"
)

func TestResourceMonitor_CPU_FailClosed(t *testing.T) {
	m := NewResourceMonitor(2, 8, 1.0)
	m.cores = 1
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return base }

	// Inject 14 samples (min is 15)
	for i := 0; i < 14; i++ {
		m.observeCPULoadAt(0.1, base.Add(time.Duration(-i)*time.Second))
	}

	ok, reason := m.CanAdmit(context.Background(), PriorityLive)
	if ok || reason != ReasonCPUUnknown {
		t.Errorf("expected fail-closed (ReasonCPUUnknown) with 14 samples, got ok=%v reason=%v", ok, reason)
	}

	// Add 15th sample
	m.observeCPULoadAt(0.1, base.Add(-14*time.Second))
	ok, reason = m.CanAdmit(context.Background(), PriorityLive)
	if !ok {
		t.Errorf("expected admit with 15 healthy samples, got %v", reason)
	}
}

func TestResourceMonitor_CPU_RatioAdmisson(t *testing.T) {
	m := NewResourceMonitor(2, 8, 1.5) // threshold = 1.0 * 1.5 = 1.5
	m.cores = 1
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return base }

	// Total 30 samples in 30s window
	// Ratio 0.5 means 15 or more over threshold rejects.

	t.Run("46%_Samples_Over_Threshold_Admits", func(t *testing.T) {
		m.cpuMu.Lock()
		m.cpuSamples = nil
		m.cpuMu.Unlock()
		// 14 over, 16 under = 14/30 = 0.46
		for i := 0; i < 14; i++ {
			m.observeCPULoadAt(2.0, base.Add(time.Duration(-i)*time.Second))
		}
		for i := 14; i < 30; i++ {
			m.observeCPULoadAt(0.5, base.Add(time.Duration(-i)*time.Second))
		}
		ok, reason := m.CanAdmit(context.Background(), PriorityLive)
		if !ok {
			t.Errorf("expected admit with 46%% pressure, got %v", reason)
		}
	})

	t.Run("50%_Samples_Over_Threshold_Rejects", func(t *testing.T) {
		m.cpuMu.Lock()
		m.cpuSamples = nil
		m.cpuMu.Unlock()
		// 15 over, 15 under = 0.5
		for i := 0; i < 15; i++ {
			m.observeCPULoadAt(2.0, base.Add(time.Duration(-i)*time.Second))
		}
		for i := 15; i < 30; i++ {
			m.observeCPULoadAt(0.5, base.Add(time.Duration(-i)*time.Second))
		}
		ok, reason := m.CanAdmit(context.Background(), PriorityLive)
		if ok || reason != ReasonCPUSaturated {
			t.Errorf("expected reject with 50%% pressure, got ok=%v reason=%v", ok, reason)
		}
	})
}

func TestResourceMonitor_CPU_EvictionBoundary(t *testing.T) {
	m := NewResourceMonitor(2, 8, 1.0)
	m.cores = 1
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return base }

	// Ensure clean state
	m.cpuMu.Lock()
	m.cpuSamples = nil
	m.cpuMu.Unlock()

	// Sample exactly at -30s should be kept when evaluated at 'base'
	m.observeCPULoadAt(0.5, base.Add(-30*time.Second))
	// Sample at -30.001s should be kept initially because observeCPULoadAt uses 'at' to prune
	m.observeCPULoadAt(0.5, base.Add(-30*time.Second-time.Millisecond))

	// Evaluate at 'base' -> should prune the -30.001s one
	m.cpuWithinLimits()

	m.cpuMu.Lock()
	count := len(m.cpuSamples)
	m.cpuMu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 sample, got %d", count)
	}

	// Move clock forward -> sample at -30s is now exactly 30s + 1ms old -> pruned
	m.clock = func() time.Time { return base.Add(time.Millisecond) }
	ok, reason := m.CanAdmit(context.Background(), PriorityLive)
	if ok || reason != ReasonCPUUnknown {
		t.Errorf("expected fail-closed after clock tick, got ok=%v reason=%v", ok, reason)
	}
}

func TestResourceMonitor_CPU_TimeJump(t *testing.T) {
	m := NewResourceMonitor(2, 8, 1.0)
	m.cores = 1
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return base }

	// Add 30 healthy samples
	for i := 0; i < 30; i++ {
		m.observeCPULoadAt(0.1, base.Add(time.Duration(-i)*time.Second))
	}

	ok, _ := m.CanAdmit(context.Background(), PriorityLive)
	if !ok {
		t.Fatal("should admit with healthy history")
	}

	// Jump clock forward 60s -> all samples should be evicted
	m.clock = func() time.Time { return base.Add(60 * time.Second) }
	ok, reason := m.CanAdmit(context.Background(), PriorityLive)
	if ok || reason != ReasonCPUUnknown {
		t.Errorf("expected fail-closed after 60s time jump, got ok=%v reason=%v", ok, reason)
	}
}
