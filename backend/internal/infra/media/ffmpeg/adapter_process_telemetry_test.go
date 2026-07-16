package ffmpeg

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/stretchr/testify/assert"
)

type mockTracer struct {
	marks map[telemetry.StartupMilestone]bool
}

func (m *mockTracer) MarkOnce(milestone telemetry.StartupMilestone, phase string, fields ...telemetry.LogField) {
	if m.marks == nil {
		m.marks = make(map[telemetry.StartupMilestone]bool)
	}
	m.marks[milestone] = true
}
func (m *mockTracer) UpdateMetadata(c, co, v, i string) {}
func (m *mockTracer) Summary()                          {}

func TestTelemetryReader_E2_T2(t *testing.T) {
	tracer := &mockTracer{}

	payload := make([]byte, 1000)
	// Create three sync bytes at offset 10
	payload[10] = 0x47
	payload[10+188] = 0x47
	payload[10+376] = 0x47

	r := &telemetryReader{
		source: bytes.NewReader(payload),
		tracer: tracer,
	}

	buf := make([]byte, 20)
	n, err := r.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 20, n)
	assert.True(t, tracer.marks[telemetry.MilestoneE2], "E2 should be marked on first read")
	assert.False(t, tracer.marks[telemetry.MilestoneT2], "T2 should not be marked yet")

	// Read enough to cover all three 0x47s
	rest := make([]byte, 1000)
	n, err = io.ReadFull(r, rest[:980])
	assert.Equal(t, 980, n)
	assert.NoError(t, err)
	assert.True(t, tracer.marks[telemetry.MilestoneT2], "T2 should be marked when 3 consecutive 0x47 spaced by 188 are found")
}

func TestTelemetryReader_NoFalsePositive(t *testing.T) {
	tracer := &mockTracer{}

	payload := make([]byte, 1000)
	// Only two sync bytes, separated by 188
	payload[10] = 0x47
	payload[10+188] = 0x47
	// Third is wrong offset
	payload[10+377] = 0x47

	r := &telemetryReader{
		source: bytes.NewReader(payload),
		tracer: tracer,
	}

	buf := make([]byte, 1000)
	io.ReadFull(r, buf)
	assert.True(t, tracer.marks[telemetry.MilestoneE2], "E2 should be marked")
	assert.False(t, tracer.marks[telemetry.MilestoneT2], "T2 should NOT be marked")
}

func TestTelemetryReader_CrossReadBoundaries(t *testing.T) {
	tracer := &mockTracer{}
	payload := make([]byte, 1000)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	payload[100] = 0x47
	payload[100+188] = 0x47
	payload[100+376] = 0x47

	r := &telemetryReader{
		source: bytes.NewReader(payload),
		tracer: tracer,
	}

	out := make([]byte, 1000)
	// Read piece by piece
	n, _ := r.Read(out[:105]) // First sync byte is inside
	assert.Equal(t, 105, n)
	assert.False(t, tracer.marks[telemetry.MilestoneT2])

	n, _ = r.Read(out[105:290]) // Second sync byte is inside (288)
	assert.Equal(t, 185, n)
	assert.False(t, tracer.marks[telemetry.MilestoneT2])

	n, _ = r.Read(out[290:480]) // Third sync byte is inside (476)
	assert.Equal(t, 190, n)
	assert.True(t, tracer.marks[telemetry.MilestoneT2])

	// verify E2 only marked once
	tracer.marks[telemetry.MilestoneE2] = false
	n, _ = r.Read(out[480:])
	assert.Equal(t, 520, n)
	assert.False(t, tracer.marks[telemetry.MilestoneE2], "E2 must not be marked again")

	// verify all bytes intact
	assert.Equal(t, payload, out)
}

func TestPrepareTelemetryPipe_DirectPath(t *testing.T) {
	adapter := &LocalAdapter{
		Config: AdapterConfig{
			StartupIngestProxy: false,
		},
	}
	r, ok := adapter.prepareTelemetryPipe(context.Background(), "http://127.0.0.1:8001/stream", "session-123")
	assert.False(t, ok)
	assert.Nil(t, r, "io.Reader must be nil when proxy is disabled")
}
