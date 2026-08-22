// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/receivertopology"
)

// TestLive_ReceiverServiceDatabaseResolvesTransponders drives the whole RF-truth
// chain against a real receiver: fetch the service database over OpenWebIF, parse
// it, and resolve a service reference to physical tuning parameters.
//
// Fixtures prove the parser; only a receiver proves the path. This is what caught
// that /api/channelinfo answers 404 on OpenWebIF 2.4.0, which no unit test could
// have shown.
//
// Skipped unless XG2G_LIVE_RECEIVER_URL points at a receiver, so an ordinary
// `go test ./...` on a machine with no receiver stays green.
func TestLive_ReceiverServiceDatabaseResolvesTransponders(t *testing.T) {
	baseURL := os.Getenv("XG2G_LIVE_RECEIVER_URL")
	if baseURL == "" {
		t.Skip("XG2G_LIVE_RECEIVER_URL not set; skipping live receiver check")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	registry := receivertopology.NewTransponderRegistry()
	registry.SetLamedbSource(openwebif.New(baseURL))

	snap, err := registry.LoadLamedb(ctx)
	if err != nil {
		t.Fatalf("load service database from %s: %v", baseURL, err)
	}
	if len(snap.Transponders) == 0 {
		t.Fatalf("receiver returned a service database with no transponders (version %d)", snap.Version)
	}
	t.Logf("lamedb version %d: %d transponders, %d skipped, %d malformed",
		snap.Version, len(snap.Transponders), snap.Skipped, snap.Malformed)

	// The service reference under test defaults to ORF1 HD, the channel the live
	// streaming work is verified against.
	serviceRef := os.Getenv("XG2G_LIVE_SREF")
	if serviceRef == "" {
		serviceRef = "1:0:19:132F:3EF:1:C00000:0:0:0:"
	}

	mux, err := registry.ResolveTransponder(ctx, serviceRef)
	if err != nil {
		t.Fatalf("resolve %s against live service database: %v", serviceRef, err)
	}
	if mux.TransponderKey == nil || mux.TransponderKey.FrequencyHz == 0 {
		t.Fatalf("resolved %s without RF parameters: %+v", serviceRef, mux)
	}
	if mux.RFPlane == nil {
		t.Fatalf("resolved %s without an RF plane: %+v", serviceRef, mux)
	}

	t.Logf("%s -> %s on plane %s", serviceRef, mux.TransponderKey.Canonical(), mux.RFPlane.String())
}
