// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bus

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func getCounterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	metric := &dto.Metric{}
	require.NoError(t, counter.Write(metric))
	return metric.GetCounter().GetValue()
}

func TestMemoryBusDropMetrics(t *testing.T) {
	b := NewMemoryBus()
	sub, err := b.Subscribe(context.Background(), "topic")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })

	initial := getCounterValue(t, metrics.BusDropsTotal.WithLabelValues("topic"))

	for i := 0; i < 100; i++ {
		_ = b.Publish(context.Background(), "topic", "msg")
	}

	final := getCounterValue(t, metrics.BusDropsTotal.WithLabelValues("topic"))
	require.Greater(t, final, initial, "expected bus drop counter to increase")
}
