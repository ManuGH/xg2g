package admission

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultCPUSampleInterval = 2 * time.Second

// CPULoadProvider returns the current system load average value.
type CPULoadProvider func() (float64, error)

// ReadSystemLoad reads the 1-minute load average from /proc/loadavg.
func ReadSystemLoad() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("loadavg parse: no fields")
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("loadavg parse: %w", err)
	}
	return load, nil
}

// StartCPUSampler begins a background sampler that feeds CPU load into the monitor.
// It stops when ctx is canceled.
func StartCPUSampler(ctx context.Context, m *ResourceMonitor, interval time.Duration, provider CPULoadProvider) {
	if m == nil {
		return
	}
	if interval <= 0 {
		interval = defaultCPUSampleInterval
	}
	if provider == nil {
		provider = ReadSystemLoad
	}

	sample := func() {
		load, err := provider()
		if err != nil {
			return
		}
		m.ObserveCPULoad(load)
	}

	// Take an immediate sample to avoid startup gaps.
	sample()

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
}
