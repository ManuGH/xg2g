// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package load provides realistic OpenWebIF mocks for load and performance testing
package load

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// MockConfig configures the behavior of the mock OpenWebIF server
type MockConfig struct {
	// Latency configuration
	MinLatency time.Duration // Minimum response latency
	MaxLatency time.Duration // Maximum response latency

	// Failure simulation
	ErrorRate       float64       // Probability of returning 500 error (0.0-1.0)
	TimeoutRate     float64       // Probability of timing out (0.0-1.0)
	TimeoutDuration time.Duration // How long to block on timeout

	// Data generation
	BouquetCount       int // Number of bouquets to generate
	ChannelsPerBouquet int // Channels per bouquet

	// Rate limiting
	MaxRequestsPerSecond int // 0 = unlimited

	// Metrics
	EnableMetrics bool
}

// DefaultMockConfig returns a realistic OpenWebIF mock configuration
func DefaultMockConfig() MockConfig {
	return MockConfig{
		MinLatency:           10 * time.Millisecond,
		MaxLatency:           50 * time.Millisecond,
		ErrorRate:            0.0,
		TimeoutRate:          0.0,
		TimeoutDuration:      30 * time.Second,
		BouquetCount:         5,
		ChannelsPerBouquet:   50,
		MaxRequestsPerSecond: 0,
		EnableMetrics:        true,
	}
}

// HighLoadConfig returns configuration for high-load scenarios
func HighLoadConfig() MockConfig {
	return MockConfig{
		MinLatency:           50 * time.Millisecond,
		MaxLatency:           200 * time.Millisecond,
		ErrorRate:            0.05, // 5% error rate
		TimeoutRate:          0.01, // 1% timeout rate
		TimeoutDuration:      10 * time.Second,
		BouquetCount:         10,
		ChannelsPerBouquet:   100,
		MaxRequestsPerSecond: 50,
		EnableMetrics:        true,
	}
}

// UnstableConfig returns configuration for resilience testing
func UnstableConfig() MockConfig {
	return MockConfig{
		MinLatency:           100 * time.Millisecond,
		MaxLatency:           500 * time.Millisecond,
		ErrorRate:            0.20, // 20% error rate
		TimeoutRate:          0.05, // 5% timeout rate
		TimeoutDuration:      5 * time.Second,
		BouquetCount:         3,
		ChannelsPerBouquet:   30,
		MaxRequestsPerSecond: 0,
		EnableMetrics:        true,
	}
}

// MockServer is a realistic OpenWebIF mock for load testing
type MockServer struct {
	config  MockConfig
	metrics *Metrics
	rng     *rand.Rand
}

// Metrics tracks mock server statistics
type Metrics struct {
	RequestsTotal   atomic.Int64
	RequestsSuccess atomic.Int64
	RequestsError   atomic.Int64
	RequestsTimeout atomic.Int64
	TotalLatency    atomic.Int64 // Microseconds
}

// NewMockServer creates a new mock OpenWebIF server
func NewMockServer(config MockConfig) *MockServer {
	return &MockServer{
		config:  config,
		metrics: &Metrics{},
		// #nosec G404 -- Test mock does not need crypto/rand
		rng: rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec
	}
}

// GetMetrics returns current metrics snapshot
func (m *MockServer) GetMetrics() MetricsSnapshot {
	total := m.metrics.RequestsTotal.Load()
	success := m.metrics.RequestsSuccess.Load()
	errors := m.metrics.RequestsError.Load()
	timeouts := m.metrics.RequestsTimeout.Load()
	latency := m.metrics.TotalLatency.Load()

	avgLatency := time.Duration(0)
	if total > 0 {
		avgLatency = time.Duration(latency / total)
	}

	return MetricsSnapshot{
		RequestsTotal:   total,
		RequestsSuccess: success,
		RequestsError:   errors,
		RequestsTimeout: timeouts,
		AverageLatency:  avgLatency,
	}
}

// MetricsSnapshot is an immutable snapshot of metrics
type MetricsSnapshot struct {
	RequestsTotal   int64
	RequestsSuccess int64
	RequestsError   int64
	RequestsTimeout int64
	AverageLatency  time.Duration
}

// Handler returns the HTTP handler for the mock server
func (m *MockServer) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", m.wrapHandler(m.handleStatus))
	mux.HandleFunc("/api/bouquets", m.wrapHandler(m.handleBouquets))
	mux.HandleFunc("/api/getservices", m.wrapHandler(m.handleGetServices))

	return mux
}

// wrapHandler applies latency, error simulation, and metrics to handlers
func (m *MockServer) wrapHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m.metrics.RequestsTotal.Add(1)

		// Simulate timeout
		if m.shouldSimulateTimeout() {
			m.metrics.RequestsTimeout.Add(1)
			time.Sleep(m.config.TimeoutDuration)
			return
		}

		// Simulate latency
		latency := m.calculateLatency()
		time.Sleep(latency)

		// Simulate error
		if m.shouldSimulateError() {
			m.metrics.RequestsError.Add(1)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Call actual handler
		handler(w, r)

		m.metrics.RequestsSuccess.Add(1)
		m.metrics.TotalLatency.Add(time.Since(start).Microseconds())
	}
}

func (m *MockServer) shouldSimulateError() bool {
	return m.rng.Float64() < m.config.ErrorRate
}

func (m *MockServer) shouldSimulateTimeout() bool {
	return m.rng.Float64() < m.config.TimeoutRate
}

func (m *MockServer) calculateLatency() time.Duration {
	if m.config.MinLatency == m.config.MaxLatency {
		return m.config.MinLatency
	}
	diff := m.config.MaxLatency - m.config.MinLatency
	return m.config.MinLatency + time.Duration(m.rng.Int63n(int64(diff)))
}

func (m *MockServer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"version": "mock-1.0.0",
	})
}

func (m *MockServer) handleBouquets(w http.ResponseWriter, _ *http.Request) {
	bouquets := make([][]string, 0, m.config.BouquetCount)
	for i := 0; i < m.config.BouquetCount; i++ {
		ref := fmt.Sprintf("1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.%d.tv\" ORDER BY bouquet", i)
		name := fmt.Sprintf("Bouquet_%d", i)
		bouquets = append(bouquets, []string{ref, name})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"bouquets": bouquets,
	})
}

func (m *MockServer) handleGetServices(w http.ResponseWriter, _ *http.Request) {
	services := make([]map[string]string, 0, m.config.ChannelsPerBouquet)

	for i := 0; i < m.config.ChannelsPerBouquet; i++ {
		channelNum := 1000 + i
		ref := fmt.Sprintf("1:0:19:%X:3EF:1:C00000:0:0:0:", channelNum)
		name := fmt.Sprintf("Channel_%d", i)

		services = append(services, map[string]string{
			"servicename":      name,
			"servicereference": ref,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"services": services,
	})
}

// RealisticChannelGenerator generates realistic channel data
type RealisticChannelGenerator struct {
	hdChannels    []string
	sdChannels    []string
	radioChannels []string
}

// NewRealisticChannelGenerator creates a generator with realistic channel names
func NewRealisticChannelGenerator() *RealisticChannelGenerator {
	return &RealisticChannelGenerator{
		hdChannels: []string{
			"ORF1 HD", "ORF2 HD", "ORF3 HD",
			"ARD HD", "ZDF HD", "RTL HD", "SAT.1 HD", "ProSieben HD",
			"ServusTV HD", "PULS 4 HD", "ATV HD",
			"BBC One HD", "BBC Two HD", "ITV HD",
			"TF1 HD", "France 2 HD", "Canal+ HD",
		},
		sdChannels: []string{
			"3sat", "ARTE", "Phoenix", "tagesschau24",
			"RTL II", "VOX", "kabel eins", "NITRO",
			"Comedy Central", "MTV", "DMAX",
		},
		radioChannels: []string{
			"Ö3", "FM4", "Kronehit",
			"Bayern 1", "Bayern 2", "Bayern 3",
			"WDR 2", "SWR3", "hr3",
		},
	}
}

// GenerateChannel returns a realistic channel at the given index
func (g *RealisticChannelGenerator) GenerateChannel(index int) (name string, ref string, channelType string) {
	// Distribute: 60% HD, 30% SD, 10% Radio
	switch {
	case index%10 < 6: // 60% HD
		idx := index % len(g.hdChannels)
		name = g.hdChannels[idx]
		channelType = "HD"
	case index%10 < 9: // 30% SD
		idx := index % len(g.sdChannels)
		name = g.sdChannels[idx]
		channelType = "SD"
	default: // 10% Radio
		idx := index % len(g.radioChannels)
		name = g.radioChannels[idx]
		channelType = "Radio"
	}

	// Generate realistic service reference
	sid := 1000 + index
	ref = fmt.Sprintf("1:0:19:%X:3EF:1:C00000:0:0:0:", sid)

	return name, ref, channelType
}

// EPGMockServer adds EPG endpoint support
type EPGMockServer struct {
	*MockServer
	epgConfig EPGConfig
}

// EPGConfig configures EPG mock behavior
type EPGConfig struct {
	EventsPerChannel int           // Number of EPG events per channel
	EventDuration    time.Duration // Average event duration
	EnableImages     bool          // Include event images
}

// DefaultEPGConfig returns default EPG configuration
func DefaultEPGConfig() EPGConfig {
	return EPGConfig{
		EventsPerChannel: 24, // 1 day of programs
		EventDuration:    2 * time.Hour,
		EnableImages:     false,
	}
}

// NewEPGMockServer creates a mock server with EPG support
func NewEPGMockServer(config MockConfig, epgConfig EPGConfig) *EPGMockServer {
	return &EPGMockServer{
		MockServer: NewMockServer(config),
		epgConfig:  epgConfig,
	}
}

// Handler returns HTTP handler with EPG endpoints
func (e *EPGMockServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// Standard endpoints
	mux.HandleFunc("/api/status", e.wrapHandler(e.handleStatus))
	mux.HandleFunc("/api/bouquets", e.wrapHandler(e.handleBouquets))
	mux.HandleFunc("/api/getservices", e.wrapHandler(e.handleGetServices))

	// EPG endpoint
	mux.HandleFunc("/api/epgservice", e.wrapHandler(e.handleEPG))

	return mux
}

func (e *EPGMockServer) handleEPG(w http.ResponseWriter, r *http.Request) {
	// Parse service reference from query
	sref := r.URL.Query().Get("sRef")
	if sref == "" {
		http.Error(w, "Missing sRef parameter", http.StatusBadRequest)
		return
	}

	// Generate EPG events
	events := make([]map[string]interface{}, 0, e.epgConfig.EventsPerChannel)
	now := time.Now()

	for i := 0; i < e.epgConfig.EventsPerChannel; i++ {
		start := now.Add(time.Duration(i) * e.epgConfig.EventDuration)

		event := map[string]interface{}{
			"id":              strconv.Itoa(1000 + i),
			"begin_timestamp": start.Unix(),
			"duration_sec":    int(e.epgConfig.EventDuration.Seconds()),
			"title":           fmt.Sprintf("Program %d", i),
			"shortdesc":       fmt.Sprintf("Description for program %d", i),
			"longdesc":        fmt.Sprintf("Long description for program %d with more details", i),
		}

		if e.epgConfig.EnableImages {
			event["image"] = fmt.Sprintf("https://example.com/image%d.jpg", i)
		}

		events = append(events, event)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
	})
}
