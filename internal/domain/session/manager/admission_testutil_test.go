package manager

import "github.com/ManuGH/xg2g/internal/admission"

func newAdmissionMonitor(maxPool, gpuLimit int, cpuScale float64) *admission.ResourceMonitor {
	m := admission.NewResourceMonitor(maxPool, gpuLimit, cpuScale)
	m.ObserveCPULoad(0.1)
	return m
}
