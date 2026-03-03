package manager

import "github.com/ManuGH/xg2g/internal/admission"

func newAdmissionMonitor(maxPool, gpuLimit int, cpuScale float64) *admission.ResourceMonitor {
	m := admission.NewResourceMonitor(maxPool, gpuLimit, cpuScale)
	// Deterministic admission for tests: ResourceMonitor fail-closed until it has >= 15 samples.
	for i := 0; i < 15; i++ {
		m.ObserveCPULoad(0.1)
	}
	return m
}
