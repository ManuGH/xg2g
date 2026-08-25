// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package topologytest supplies real receiver-derived transponder facts to tests
// across the codebase.
//
// Topology tests assert on RF planes, which only mean something if the carriers
// are real. Before this existed, a table of hand-written broadcast frequencies was
// loaded into every topology service by default; when it was finally checked against
// the receiver, every entry in it was wrong. Tests seeded from that table were
// agreeing with a fiction. The fixtures here are captures of the actual service
// database of the Vu+ Uno 4K (OpenATV, OWIF 2.4.0) this project is developed against,
// so a test that passes against them is a test that would pass against the hardware.
package topologytest

import (
	_ "embed"
	"testing"

	"github.com/ManuGH/xg2g/internal/receivertopology"
)

// VuUno4KLamedbV4 is the transponder section of the receiver's /etc/enigma2/lamedb.
//
//go:embed lamedb_v4_vuuno4k.txt
var VuUno4KLamedbV4 []byte

// VuUno4KLamedbV5 is the same 161 carriers as written to /etc/enigma2/lamedb5.
//
//go:embed lamedb5_vuuno4k.txt
var VuUno4KLamedbV5 []byte

// Registry returns a transponder registry holding every carrier the reference
// receiver knows about.
func Registry(t *testing.T) *receivertopology.TransponderRegistry {
	t.Helper()

	registry := receivertopology.NewTransponderRegistry()
	snap, err := registry.LoadLamedbBytes(VuUno4KLamedbV4)
	if err != nil {
		t.Fatalf("load reference receiver service database: %v", err)
	}
	if len(snap.Transponders) == 0 {
		t.Fatal("reference receiver service database yielded no transponders")
	}
	return registry
}

// SeedService points a topology service at the reference receiver's transponder facts.
func SeedService(t *testing.T, svc *receivertopology.Service) {
	t.Helper()
	svc.SetResolver(Registry(t))
}
