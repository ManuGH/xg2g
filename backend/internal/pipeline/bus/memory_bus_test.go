// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bus

import (
	"context"
	"testing"
	"time"

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

func TestMemoryBusPublishContextTimeoutIncrementsDropMetrics(t *testing.T) {
	b := NewMemoryBus()
	sub, err := b.Subscribe(context.Background(), "topic")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })

	// Fill subscriber channel to capacity so next publish blocks.
	for i := 0; i < cap(sub.C()); i++ {
		require.NoError(t, b.Publish(context.Background(), "topic", "msg"))
	}

	initialLegacy := getCounterValue(t, metrics.BusDropsTotal.WithLabelValues("topic"))
	initialReasoned := getCounterValue(t, metrics.BusDroppedTotal.WithLabelValues("topic", "timeout"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = b.Publish(ctx, "topic", "blocked")
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	finalLegacy := getCounterValue(t, metrics.BusDropsTotal.WithLabelValues("topic"))
	finalReasoned := getCounterValue(t, metrics.BusDroppedTotal.WithLabelValues("topic", "timeout"))
	require.Greater(t, finalLegacy, initialLegacy, "expected legacy bus drop counter to increase")
	require.Greater(t, finalReasoned, initialReasoned, "expected reasoned bus drop counter to increase")
}

func TestMemoryBusPublishRejectsNilContext(t *testing.T) {
	b := NewMemoryBus()
	err := b.Publish(nil, "topic", "msg")
	require.Error(t, err)
	require.Contains(t, err.Error(), "context is nil")
}
