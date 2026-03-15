package admission

import (
	"testing"
	"time"
)

func TestResourceMonitorSnapshot_ReflectsLatestCPULoadAndTokens(t *testing.T) {
	m := NewResourceMonitor(2, 8, 1.5)
	m.cores = 6
	m.cpuWindow = 42 * time.Second

	m.observeCPULoadAt(0.4, time.Unix(0, 0).UTC())
	m.observeCPULoadAt(1.7, time.Unix(10, 0).UTC())
	if !m.AcquireVAAPIToken() {
		t.Fatal("expected first VAAPI token acquisition to succeed")
	}
	if !m.AcquireVAAPIToken() {
		t.Fatal("expected second VAAPI token acquisition to succeed")
	}

	got := m.Snapshot()

	if got.CPU.Load1m != 1.7 {
		t.Fatalf("expected latest CPU load to be reported, got %v", got.CPU.Load1m)
	}
	if got.CPU.CoreCount != 6 {
		t.Fatalf("expected core count 6, got %d", got.CPU.CoreCount)
	}
	if got.CPU.SampleCount != 2 {
		t.Fatalf("expected sample count 2, got %d", got.CPU.SampleCount)
	}
	if got.CPU.WindowSeconds != 42 {
		t.Fatalf("expected window seconds 42, got %d", got.CPU.WindowSeconds)
	}
	if got.ActiveVAAPITokens != 2 {
		t.Fatalf("expected 2 active VAAPI tokens, got %d", got.ActiveVAAPITokens)
	}
	if got.MaxSessions != 2 || got.MaxVAAPITokens != 8 {
		t.Fatalf("expected monitor limits to be reflected, got %#v", got)
	}
}

func TestResourceMonitorSnapshot_EmptyHistoryIsStable(t *testing.T) {
	m := NewResourceMonitor(2, 8, 1.5)
	got := m.Snapshot()

	if got.CPU.Load1m != 0 || got.CPU.SampleCount != 0 {
		t.Fatalf("expected empty CPU history to stay zeroed, got %#v", got.CPU)
	}
	if got.ActiveVAAPITokens != 0 {
		t.Fatalf("expected no active VAAPI tokens, got %d", got.ActiveVAAPITokens)
	}
}
