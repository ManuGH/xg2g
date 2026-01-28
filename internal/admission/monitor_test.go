package admission

import (
	"context"
	"testing"
)

func TestResourceMonitor_GPUAdmission(t *testing.T) {
	m := NewResourceMonitor(2, 8, 1.5) // maxPool=2, gpuLimit=8
	for i := 0; i < 20; i++ {
		m.ObserveCPULoad(0.1)
	}

	// 1. Admit first two
	if ok, _ := m.CanAdmit(context.Background(), PriorityLive); !ok {
		t.Fatal("Should admit first Live session")
	}
	m.TrackSessionStart(PriorityLive, "s1")
	m.AcquireVAAPIToken()

	if ok, _ := m.CanAdmit(context.Background(), PriorityLive); !ok {
		t.Fatal("Should admit second Live session")
	}
	m.TrackSessionStart(PriorityLive, "s2")
	m.AcquireVAAPIToken()

	// 3. Reject Pulse at pool limit (pool full, no preemptible lower-priority sessions)
	if ok, reason := m.CanAdmit(context.Background(), PriorityPulse); ok {
		t.Fatal("Should reject Pulse when pool limit reached")
	} else if reason != ReasonPoolFull {
		t.Fatalf("Expected PoolFull reason, got %v", reason)
	}

	// 4. Release session and token, then check
	m.TrackSessionEnd(PriorityLive, "s2")
	m.ReleaseVAAPIToken()
	if ok, _ := m.CanAdmit(context.Background(), PriorityPulse); !ok {
		t.Fatal("Should admit Pulse after session release")
	}
}

func TestResourceMonitor_PriorityOrdering(t *testing.T) {
	// Priority codes check
	if PriorityRecording <= PriorityLive {
		t.Error("Recording priority must be > Live")
	}
	if PriorityLive <= PriorityPulse {
		t.Error("Live priority must be > Pulse")
	}
}

func TestResourceMonitor_TokenCleanup(t *testing.T) {
	m := NewResourceMonitor(8, 1, 1.5) // maxPool=8, gpuLimit=1 (testing token limit)
	for i := 0; i < 20; i++ {
		m.ObserveCPULoad(0.1)
	}

	if !m.AcquireVAAPIToken() {
		t.Fatal("Should acquire token")
	}

	if m.AcquireVAAPIToken() {
		t.Fatal("Should not acquire second token")
	}

	m.ReleaseVAAPIToken()

	if !m.AcquireVAAPIToken() {
		t.Fatal("Should re-acquire token after release")
	}
}

func TestResourceMonitor_TunerAdmission(t *testing.T) {
	m := NewResourceMonitor(4, 8, 1.5) // maxPool=4, gpuLimit=8
	for i := 0; i < 20; i++ {
		m.ObserveCPULoad(0.1)
	}

	// 1. Fill 4 sessions with Live
	for i := 0; i < 4; i++ {
		m.TrackSessionStart(PriorityLive, "l"+string(rune(i)))
	}

	// 2. Reject another Live (pool full, no preemptible)
	if ok, reason := m.CanAdmit(context.Background(), PriorityLive); ok {
		t.Fatal("Should reject Live when pool full")
	} else if reason != ReasonPoolFull {
		t.Fatalf("Expected PoolFull reason, got %v", reason)
	}

	// 3. Admit Recording via preemption
	if ok, _ := m.CanAdmit(context.Background(), PriorityRecording); !ok {
		t.Fatal("Should admit Recording when preemption is possible")
	}
}

func TestResourceMonitor_PreemptionPredicates(t *testing.T) {
	m := NewResourceMonitor(8, 8, 1.5) // maxPool=8, gpuLimit=8
	for i := 0; i < 20; i++ {
		m.ObserveCPULoad(0.1)
	}

	// Pulse < Live < Recording
	m.TrackSessionStart(PriorityPulse, "p1")
	m.TrackSessionStart(PriorityLive, "l1")

	// Recording should find Pulse as best target
	id, ok := m.SelectPreemptionTarget(PriorityRecording)
	if !ok || id != "p1" {
		t.Fatalf("Expected p1 target, got %v (ok=%v)", id, ok)
	}

	// Clean Pulse
	m.TrackSessionEnd(PriorityPulse, "p1")

	// Recording should find Live as best target
	id, ok = m.SelectPreemptionTarget(PriorityRecording)
	if !ok || id != "l1" {
		t.Fatalf("Expected l1 target, got %v (ok=%v)", id, ok)
	}
}
