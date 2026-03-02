// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package testkit

import "github.com/ManuGH/xg2g/internal/admission"

func NewAdmissibleAdmission() *admission.ResourceMonitor {
	m := admission.NewResourceMonitor(1000, 1000, 10)
	for i := 0; i < 15; i++ {
		m.ObserveCPULoad(0.1)
	}
	return m
}
