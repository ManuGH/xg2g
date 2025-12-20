// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build benchmark
// +build benchmark

package benchmark

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// LatencyTransport simulates network latency
type LatencyTransport struct {
	Latency time.Duration
}

func (t *LatencyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	time.Sleep(t.Latency)
	// Return a fake 200 OK response
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
	}, nil
}

func BenchmarkInstantTune(b *testing.B) {
	// Simulate network latency for "tuning"
	latency := 50 * time.Millisecond

	logger := zerolog.Nop()
	// Host doesn't matter since we mock the transport
	detector := openwebif.NewStreamDetector("127.0.0.1", logger)

	// Inject mock client with latency
	mockClient := &http.Client{
		Transport: &LatencyTransport{Latency: latency},
	}
	detector.SetHTTPClient(mockClient)

	services := make([][2]string, 100)
	for i := 0; i < 100; i++ {
		services[i] = [2]string{"Service " + string(rune(i)), fmt.Sprintf("1:0:1:%d:0:0:0:0:0:0:", i)}
	}

	b.Run("ColdCache_Sequential", func(b *testing.B) {
		// For cold cache, we need a fresh detector each time or clear cache
		// But creating new detector is cheap.

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				// Create fresh detector to simulate cold cache
				d := openwebif.NewStreamDetector("127.0.0.1", logger)
				d.SetHTTPClient(mockClient)
				ctx := context.Background()

				// Test just one service
				_, _ = d.DetectStreamURL(ctx, "1:0:1:79E0:443:1:C00000:0:0:0:", "Test Channel", false)
			}
		})
	})

	b.Run("WarmCache_InstantTune", func(b *testing.B) {
		// Pre-warm cache
		ctx := context.Background()
		_, _ = detector.DetectBatch(ctx, services)

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			s := services[0]
			// With Instant Tune, the URL is already in cache
			_, _ = detector.DetectStreamURL(ctx, s[1], s[0], false)
		}
	})
}
