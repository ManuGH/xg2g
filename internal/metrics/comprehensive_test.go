// SPDX-License-Identifier: MIT
package metrics

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to get metric value from a gauge
func getGaugeValue(t *testing.T, gauge prometheus.Gauge) float64 {
	t.Helper()
	metric := &dto.Metric{}
	err := gauge.Write(metric)
	require.NoError(t, err)
	return metric.GetGauge().GetValue()
}

// Helper function to get metric value from a counter
func getCounterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	metric := &dto.Metric{}
	err := counter.Write(metric)
	require.NoError(t, err)
	return metric.GetCounter().GetValue()
}

// Helper function to get metric value from a labeled gauge
func getGaugeVecValue(t *testing.T, gaugeVec *prometheus.GaugeVec, labels ...string) float64 {
	t.Helper()
	gauge := gaugeVec.WithLabelValues(labels...)
	return getGaugeValue(t, gauge)
}

// Helper function to get metric value from a labeled counter
func getCounterVecValue(t *testing.T, counterVec *prometheus.CounterVec, labels ...string) float64 {
	t.Helper()
	counter := counterVec.WithLabelValues(labels...)
	return getCounterValue(t, counter)
}

func TestRecordBouquetsCount(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{"zero bouquets", 0},
		{"single bouquet", 1},
		{"multiple bouquets", 5},
		{"large count", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RecordBouquetsCount(tt.count)
			value := getGaugeValue(t, bouquetsTotal)
			assert.Equal(t, float64(tt.count), value)
		})
	}
}

func TestIncBouquetDiscoveryError(t *testing.T) {
	// Get initial value
	initialValue := getCounterValue(t, bouquetDiscoveryErrors)

	// Increment multiple times
	iterations := 3
	for i := 0; i < iterations; i++ {
		IncBouquetDiscoveryError()
	}

	// Verify counter increased correctly
	finalValue := getCounterValue(t, bouquetDiscoveryErrors)
	assert.Equal(t, initialValue+float64(iterations), finalValue)
}

func TestRecordServicesCount(t *testing.T) {
	tests := []struct {
		bouquet string
		count   int
	}{
		{"Favourites", 10},
		{"Sports", 25},
		{"Movies", 0},
		{"News", 5},
	}

	for _, tt := range tests {
		t.Run(tt.bouquet, func(t *testing.T) {
			RecordServicesCount(tt.bouquet, tt.count)
			value := getGaugeVecValue(t, servicesDiscovered, tt.bouquet)
			assert.Equal(t, float64(tt.count), value)
		})
	}
}

func TestIncServicesResolution(t *testing.T) {
	testCases := []struct {
		bouquet string
		outcome string
	}{
		{"Favourites", "success"},
		{"Favourites", "failure"},
		{"Sports", "success"},
		{"Movies", "failure"},
	}

	// Record initial values
	initialValues := make(map[string]float64)
	for _, tc := range testCases {
		key := tc.bouquet + "_" + tc.outcome
		initialValues[key] = getCounterVecValue(t, servicesResolutionTotal, tc.bouquet, tc.outcome)
	}

	// Increment each combination
	for _, tc := range testCases {
		IncServicesResolution(tc.bouquet, tc.outcome)
	}

	// Verify increments
	for _, tc := range testCases {
		key := tc.bouquet + "_" + tc.outcome
		expectedValue := initialValues[key] + 1
		actualValue := getCounterVecValue(t, servicesResolutionTotal, tc.bouquet, tc.outcome)
		assert.Equal(t, expectedValue, actualValue,
			"Expected %s resolution count for bouquet %s to increase by 1", tc.outcome, tc.bouquet)
	}
}

func TestIncStreamURLBuild(t *testing.T) {
	outcomes := []string{"success", "failure"}

	// Record initial values
	initialValues := make(map[string]float64)
	for _, outcome := range outcomes {
		initialValues[outcome] = getCounterVecValue(t, streamURLBuildTotal, outcome)
	}

	// Increment each outcome
	iterations := 2
	for _, outcome := range outcomes {
		for i := 0; i < iterations; i++ {
			IncStreamURLBuild(outcome)
		}
	}

	// Verify increments
	for _, outcome := range outcomes {
		expectedValue := initialValues[outcome] + float64(iterations)
		actualValue := getCounterVecValue(t, streamURLBuildTotal, outcome)
		assert.Equal(t, expectedValue, actualValue)
	}
}

func TestRecordChannelTypeCounts(t *testing.T) {
	tests := []struct {
		name    string
		hd      int
		sd      int
		radio   int
		unknown int
	}{
		{"all zeros", 0, 0, 0, 0},
		{"mixed counts", 10, 20, 5, 2},
		{"only HD", 100, 0, 0, 0},
		{"only radio", 0, 0, 50, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RecordChannelTypeCounts(tt.hd, tt.sd, tt.radio, tt.unknown)

			assert.Equal(t, float64(tt.hd), getGaugeVecValue(t, channelTypes, "hd"))
			assert.Equal(t, float64(tt.sd), getGaugeVecValue(t, channelTypes, "sd"))
			assert.Equal(t, float64(tt.radio), getGaugeVecValue(t, channelTypes, "radio"))
			assert.Equal(t, float64(tt.unknown), getGaugeVecValue(t, channelTypes, "unknown"))
		})
	}
}

func TestRecordXMLTV(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		channels int
		writeErr error
	}{
		{
			name:     "XMLTV disabled",
			enabled:  false,
			channels: 0,
			writeErr: nil,
		},
		{
			name:     "XMLTV enabled with success",
			enabled:  true,
			channels: 25,
			writeErr: nil,
		},
		{
			name:     "XMLTV enabled with error",
			enabled:  true,
			channels: 15,
			writeErr: errors.New("write failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial error count
			initialErrors := getCounterValue(t, xmltvWriteErrors)

			RecordXMLTV(tt.enabled, tt.channels, tt.writeErr)

			if tt.enabled {
				assert.Equal(t, float64(1), getGaugeValue(t, xmltvEnabled))
				assert.Equal(t, float64(tt.channels), getGaugeValue(t, xmltvChannelsWritten))

				if tt.writeErr != nil {
					expectedErrors := initialErrors + 1
					assert.Equal(t, expectedErrors, getCounterValue(t, xmltvWriteErrors))
				}
			} else {
				assert.Equal(t, float64(0), getGaugeValue(t, xmltvEnabled))
				assert.Equal(t, float64(0), getGaugeValue(t, xmltvChannelsWritten))
			}
		})
	}
}

func TestIncConfigValidationError(t *testing.T) {
	initialValue := getCounterValue(t, configValidationErrors)

	iterations := 5
	for i := 0; i < iterations; i++ {
		IncConfigValidationError()
	}

	expectedValue := initialValue + float64(iterations)
	actualValue := getCounterValue(t, configValidationErrors)
	assert.Equal(t, expectedValue, actualValue)
}

func TestIncRefreshFailure(t *testing.T) {
	stages := []string{"config", "bouquets", "services", "streamurl", "write_m3u", "xmltv"}

	// Record initial values
	initialValues := make(map[string]float64)
	for _, stage := range stages {
		initialValues[stage] = getCounterVecValue(t, refreshFailuresTotal, stage)
	}

	// Increment each stage
	for _, stage := range stages {
		IncRefreshFailure(stage)
	}

	// Verify increments
	for _, stage := range stages {
		expectedValue := initialValues[stage] + 1
		actualValue := getCounterVecValue(t, refreshFailuresTotal, stage)
		assert.Equal(t, expectedValue, actualValue, "Stage %s should have incremented by 1", stage)
	}
}

func TestEPGMetrics(t *testing.T) {
	t.Run("EPG channel error", func(t *testing.T) {
		initialErrors := getCounterVecValue(t, epgRequestsTotal, "error")

		IncEPGChannelError()

		expectedErrors := initialErrors + 1
		actualErrors := getCounterVecValue(t, epgRequestsTotal, "error")
		assert.Equal(t, expectedErrors, actualErrors)
	})

	t.Run("EPG channel success", func(t *testing.T) {
		initialSuccess := getCounterVecValue(t, epgRequestsTotal, "success")
		programmes := 10

		RecordEPGChannelSuccess(programmes)

		expectedSuccess := initialSuccess + 1
		actualSuccess := getCounterVecValue(t, epgRequestsTotal, "success")
		assert.Equal(t, expectedSuccess, actualSuccess)
	})

	t.Run("EPG collection recording", func(t *testing.T) {
		totalProgrammes := 500
		channelsWithData := 25
		duration := 5.5

		RecordEPGCollection(totalProgrammes, channelsWithData, duration)

		assert.Equal(t, float64(totalProgrammes), getGaugeValue(t, epgProgrammesCollected))
		assert.Equal(t, float64(channelsWithData), getGaugeValue(t, epgChannelsWithData))

		// For histogram, we just verify it doesn't panic and accepts the value
		// Getting the exact value from histogram is more complex and not critical for this test
		metric := &dto.Metric{}
		err := epgCollectionDurationSeconds.Write(metric)
		require.NoError(t, err)
		assert.NotNil(t, metric.GetHistogram())
		assert.True(t, metric.GetHistogram().GetSampleCount() > 0)
	})
}

// TestMetricsIntegration tests a complete workflow using multiple metrics
func TestMetricsIntegration(t *testing.T) {
	// Simulate a complete refresh workflow

	// 1. Record bouquet discovery
	RecordBouquetsCount(3)

	// 2. Record services for each bouquet
	RecordServicesCount("Favourites", 15)
	RecordServicesCount("Sports", 10)
	RecordServicesCount("Movies", 20)

	// 3. Record some successful and failed service resolutions
	IncServicesResolution("Favourites", "success")
	IncServicesResolution("Favourites", "success")
	IncServicesResolution("Sports", "failure")

	// 4. Record stream URL builds
	IncStreamURLBuild("success")
	IncStreamURLBuild("success")

	// 5. Record channel types
	RecordChannelTypeCounts(25, 15, 5, 0)

	// 6. Record XMLTV with success
	RecordXMLTV(true, 45, nil)

	// 7. Record EPG collection
	RecordEPGCollection(1200, 40, 3.2)

	// Verify all metrics are updated correctly
	assert.Equal(t, float64(3), getGaugeValue(t, bouquetsTotal))
	assert.Equal(t, float64(15), getGaugeVecValue(t, servicesDiscovered, "Favourites"))
	assert.Equal(t, float64(10), getGaugeVecValue(t, servicesDiscovered, "Sports"))
	assert.Equal(t, float64(20), getGaugeVecValue(t, servicesDiscovered, "Movies"))

	assert.Equal(t, float64(25), getGaugeVecValue(t, channelTypes, "hd"))
	assert.Equal(t, float64(15), getGaugeVecValue(t, channelTypes, "sd"))
	assert.Equal(t, float64(5), getGaugeVecValue(t, channelTypes, "radio"))
	assert.Equal(t, float64(0), getGaugeVecValue(t, channelTypes, "unknown"))

	assert.Equal(t, float64(1), getGaugeValue(t, xmltvEnabled))
	assert.Equal(t, float64(45), getGaugeValue(t, xmltvChannelsWritten))

	assert.Equal(t, float64(1200), getGaugeValue(t, epgProgrammesCollected))
	assert.Equal(t, float64(40), getGaugeValue(t, epgChannelsWithData))
}

// BenchmarkMetricOperations benchmarks common metric operations
func BenchmarkMetricOperations(b *testing.B) {
	b.Run("RecordBouquetsCount", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			RecordBouquetsCount(i % 100)
		}
	})

	b.Run("IncBouquetDiscoveryError", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			IncBouquetDiscoveryError()
		}
	})

	b.Run("RecordServicesCount", func(b *testing.B) {
		bouquets := []string{"Favourites", "Sports", "Movies", "News"}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			RecordServicesCount(bouquets[i%len(bouquets)], i%50)
		}
	})

	b.Run("IncStreamURLBuild", func(b *testing.B) {
		outcomes := []string{"success", "failure"}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			IncStreamURLBuild(outcomes[i%len(outcomes)])
		}
	})

	b.Run("RecordChannelTypeCounts", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			RecordChannelTypeCounts(i%20, i%15, i%5, i%3)
		}
	})
}
