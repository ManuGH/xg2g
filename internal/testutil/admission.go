package testutil

import "github.com/ManuGH/xg2g/internal/admission"

// NewAdmissionMonitorForTest creates a ResourceMonitor seeded with safe CPU load.
func NewAdmissionMonitorForTest(maxPool, gpuLimit int, cpuScale float64) *admission.ResourceMonitor {
	m := admission.NewResourceMonitor(maxPool, gpuLimit, cpuScale)
	m.ObserveCPULoad(0.1)
	return m
}
