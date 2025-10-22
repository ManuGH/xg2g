// SPDX-License-Identifier: MIT
package openwebif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// MockServer provides a configurable OpenWebIF mock server for testing.
type MockServer struct {
	*httptest.Server
	mu        sync.RWMutex
	bouquets  map[string]string
	services  map[string][][2]string
	epgEvents map[string][]EPGEvent
	streamURL string
	delay     map[string]int // Artificial delay in milliseconds per endpoint
	failures  map[string]int // Number of failures before success per endpoint
}

// BouquetsResponse matches OpenWebIF API structure.
type BouquetsResponse struct {
	Services [][]interface{} `json:"services"`
}

// ServicesResponse matches OpenWebIF API structure.
type ServicesResponse struct{
	Services []map[string]interface{} `json:"services"`
}

// NewMockServer creates a new OpenWebIF mock server.
func NewMockServer() *MockServer {
	mock := &MockServer{
		bouquets:  make(map[string]string),
		services:  make(map[string][][2]string),
		epgEvents: make(map[string][]EPGEvent),
		delay:     make(map[string]int),
		failures:  make(map[string]int),
	}

	// Set up default test data
	mock.SetDefaultData()

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bouquets", mock.handleBouquets)           // Modern endpoint
	mux.HandleFunc("/api/getallservices", mock.handleBouquets)     // Legacy endpoint
	mux.HandleFunc("/api/getservices", mock.handleServices)
	mux.HandleFunc("/api/epgservice", mock.handleEPG)
	mux.HandleFunc("/api/zap", mock.handleZap)
	mux.HandleFunc("/api/about", mock.handleAbout)

	mock.Server = httptest.NewServer(mux)
	return mock
}

// SetDefaultData sets up realistic test data.
func (m *MockServer) SetDefaultData() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setDefaultDataNoLock()
}

// setDefaultDataNoLock sets default data without acquiring the lock.
// This is used internally by Reset() which already holds the lock.
func (m *MockServer) setDefaultDataNoLock() {
	// Add bouquets
	m.bouquets = map[string]string{
		"1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.favourites.tv\" ORDER BY bouquet": "Favourites (TV)",
		"1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.premium.tv\" ORDER BY bouquet":     "Premium",
		"1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.hd.tv\" ORDER BY bouquet":           "HD Channels",
	}

	// Add services for Premium bouquet
	m.services["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.premium.tv\" ORDER BY bouquet"] = [][2]string{
		{"1:0:19:283D:3FB:1:C00000:0:0:0:", "ARD HD"},
		{"1:0:19:283E:3FB:1:C00000:0:0:0:", "ZDF HD"},
		{"1:0:1:6DCA:44D:1:C00000:0:0:0:", "RTL"},
		{"1:0:1:6DCB:44D:1:C00000:0:0:0:", "Pro7"},
	}

	// Add services for HD Channels bouquet
	m.services["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.hd.tv\" ORDER BY bouquet"] = [][2]string{
		{"1:0:19:283D:3FB:1:C00000:0:0:0:", "ARD HD"},
		{"1:0:19:283E:3FB:1:C00000:0:0:0:", "ZDF HD"},
		{"1:0:19:2855:401:1:C00000:0:0:0:", "RTL HD"},
	}

	// Add EPG events for ARD HD
	m.epgEvents["1:0:19:283D:3FB:1:C00000:0:0:0:"] = []EPGEvent{
		{
			ID:          32845,
			Title:       "Tagesschau",
			Description: "Nachrichten und Wetterbericht",
			Begin:       1700000000,
			Duration:    900,
		},
		{
			ID:          32846,
			Title:       "Tatort",
			Description: "Krimiserie",
			Begin:       1700000900,
			Duration:    5400,
		},
	}

	m.streamURL = "http://localhost:8001"
}

// AddBouquet adds a bouquet to the mock server.
func (m *MockServer) AddBouquet(ref, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bouquets[ref] = name
}

// AddService adds a service to a bouquet.
func (m *MockServer) AddService(bouquetRef, serviceRef, serviceName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services[bouquetRef] = append(m.services[bouquetRef], [2]string{serviceRef, serviceName})
}

// AddEPGEvent adds an EPG event for a service.
func (m *MockServer) AddEPGEvent(serviceRef string, event EPGEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.epgEvents[serviceRef] = append(m.epgEvents[serviceRef], event)
}

// SetDelay sets an artificial delay (in milliseconds) for an endpoint.
func (m *MockServer) SetDelay(endpoint string, delayMS int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delay[endpoint] = delayMS
}

// SetFailures sets the number of failures before success for an endpoint.
func (m *MockServer) SetFailures(endpoint string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[endpoint] = count
}

// handleBouquets handles /api/bouquets and /api/getallservices
func (m *MockServer) handleBouquets(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check for artificial failures
	if failures, ok := m.failures["/api/bouquets"]; ok && failures > 0 {
		m.mu.RUnlock()
		m.mu.Lock()
		m.failures["/api/bouquets"]--
		m.mu.Unlock()
		m.mu.RLock()
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Apply artificial delay
	if delay, ok := m.delay["/api/bouquets"]; ok && delay > 0 {
		// Time sleep simulation would go here in real implementation
	}

	// Build response in OpenWebIF format: [["<ref>", "<name>"], ...]
	// Client expects: name -> ref mapping from bouquets[name] = ref
	bouquets := make([][]string, 0, len(m.bouquets))
	for ref, name := range m.bouquets {
		bouquets = append(bouquets, []string{ref, name})
	}

	resp := map[string]interface{}{
		"bouquets": bouquets,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleServices handles /api/getservices
func (m *MockServer) handleServices(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bouquetRef := r.URL.Query().Get("sRef")
	if bouquetRef == "" {
		http.Error(w, "Missing sRef parameter", http.StatusBadRequest)
		return
	}

	// Check for artificial failures
	if failures, ok := m.failures["/api/getservices"]; ok && failures > 0 {
		m.mu.RUnlock()
		m.mu.Lock()
		m.failures["/api/getservices"]--
		m.mu.Unlock()
		m.mu.RLock()
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	services, ok := m.services[bouquetRef]
	if !ok {
		http.Error(w, "Bouquet not found", http.StatusNotFound)
		return
	}

	// Build response
	servicesList := make([]map[string]interface{}, len(services))
	for i, svc := range services {
		servicesList[i] = map[string]interface{}{
			"servicereference": svc[0],
			"servicename":      svc[1],
		}
	}

	resp := ServicesResponse{Services: servicesList}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleEPG handles /api/epgservice
func (m *MockServer) handleEPG(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	serviceRef := r.URL.Query().Get("sRef")
	if serviceRef == "" {
		http.Error(w, "Missing sRef parameter", http.StatusBadRequest)
		return
	}

	events, ok := m.epgEvents[serviceRef]
	if !ok {
		// Return empty EPG if no events configured
		resp := EPGResponse{Events: []EPGEvent{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Build response - use the events directly as they match EPGEvent struct
	resp := EPGResponse{Events: events}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleZap handles /api/zap (channel switching)
func (m *MockServer) handleZap(w http.ResponseWriter, r *http.Request) {
	serviceRef := r.URL.Query().Get("sRef")
	if serviceRef == "" {
		http.Error(w, "Missing sRef parameter", http.StatusBadRequest)
		return
	}

	resp := map[string]interface{}{
		"result": true,
		"message": "Channel switched successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAbout handles /api/about
func (m *MockServer) handleAbout(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"info": map[string]interface{}{
			"model":      "Mock Receiver",
			"brand":      "MockBrand",
			"boxtype":    "mockbox",
			"enigmaver":  "OpenWebIF Mock 1.0.0",
			"webifver":   "OWIF 1.4.9",
			"imagever":   "Test Image",
			"kernelver":  "5.10.0",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Reset clears all mock data and resets to defaults.
func (m *MockServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear all data
	m.bouquets = make(map[string]string)
	m.services = make(map[string][][2]string)
	m.epgEvents = make(map[string][]EPGEvent)
	m.delay = make(map[string]int)
	m.failures = make(map[string]int)

	// Reset to defaults without re-locking
	m.setDefaultDataNoLock()
}

// URL returns the mock server's base URL.
func (m *MockServer) URL() string {
	return m.Server.URL
}
