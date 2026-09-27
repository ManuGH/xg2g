// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// OpenATVAssertionMessage is the exact assertion crash triggered in OpenATV 7.6.0 on tuner over-allocation.
const OpenATVAssertionMessage = "dvb/dvb.cpp:1452 ASSERTION cnt == 1 FAILED!"

// DigitalTwinConfig defines the hardware and firmware profile for the Enigma2 Digital Twin.
type DigitalTwinConfig struct {
	Model                  string
	Brand                  string
	BoxType                string
	EnigmaVersion          string
	WebIFVersion           string
	ImageVersion           string
	KernelVersion          string
	PhysicalTuners         int
	DemuxersPerTuner       int
	CrashOnTunerExhaustion bool
	CAMGraceWindow         time.Duration
	MaxStreamBatches       int // 0 = continuous until disconnect
}

// DefaultVuUno4KConfig returns a realistic profile matching a Vu+ Uno 4K running OpenATV 7.6.0.
func DefaultVuUno4KConfig() DigitalTwinConfig {
	return DigitalTwinConfig{
		Model:                  "Vu+ Uno 4K",
		Brand:                  "Vu+",
		BoxType:                "vuuno4k",
		EnigmaVersion:          "2026-09-20",
		WebIFVersion:           "OWIF 2.1.1",
		ImageVersion:           "OpenATV 7.6.0",
		KernelVersion:          "4.1.20",
		PhysicalTuners:         2,
		DemuxersPerTuner:       4,
		CrashOnTunerExhaustion: true,
		CAMGraceWindow:         0,
		MaxStreamBatches:       0,
	}
}

// DigitalTwinOption configures a DigitalTwin instance.
type DigitalTwinOption func(*DigitalTwinConfig)

func WithPhysicalTuners(n int) DigitalTwinOption {
	return func(c *DigitalTwinConfig) {
		if n > 0 {
			c.PhysicalTuners = n
		}
	}
}

func WithCrashOnTunerExhaustion(crash bool) DigitalTwinOption {
	return func(c *DigitalTwinConfig) {
		c.CrashOnTunerExhaustion = crash
	}
}

func WithCAMGraceWindow(d time.Duration) DigitalTwinOption {
	return func(c *DigitalTwinConfig) {
		c.CAMGraceWindow = d
	}
}

func WithMaxStreamBatches(batches int) DigitalTwinOption {
	return func(c *DigitalTwinConfig) {
		c.MaxStreamBatches = batches
	}
}

func WithDemuxersPerTuner(n int) DigitalTwinOption {
	return func(c *DigitalTwinConfig) {
		if n > 0 {
			c.DemuxersPerTuner = n
		}
	}
}

// TunerAllocation tracks a physical tuner assigned to a DVB transponder.
type TunerAllocation struct {
	PhysicalTunerID int
	TransponderKey  string
	ActiveStreams   map[string]int // serviceRef -> subscriber count
}

// DigitalTwin is a high-fidelity simulation of an Enigma2 receiver with tuner governance and streaming.
type DigitalTwin struct {
	server *httptest.Server
	config DigitalTwinConfig

	mu                sync.RWMutex
	allocations       map[string]*TunerAllocation // transponderKey -> allocation
	tunerSlots        []bool                      // index = tunerID, true = in use
	currentServiceRef string
	isStandby         bool
	snrSignal         int
	isLocked          bool
	hasPIDs           bool

	// Forensic & assertion tracking
	peakTunersUsed  int
	totalStreams    atomic.Int64
	totalZaps       atomic.Int64
	assertionFailed bool
	assertionLog    []string

	// EPG & Bouquet mock data
	bouquets  map[string]string
	services  map[string][][2]string
	epgEvents map[string][]EPGEvent
}

// NewDigitalTwin creates an Enigma2 Digital Twin with the given options.
func NewDigitalTwin(opts ...DigitalTwinOption) *DigitalTwin {
	cfg := DefaultVuUno4KConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	dt := &DigitalTwin{
		config:      cfg,
		allocations: make(map[string]*TunerAllocation),
		tunerSlots:  make([]bool, cfg.PhysicalTuners),
		snrSignal:   88,
		isLocked:    true,
		hasPIDs:     true,
		bouquets:    make(map[string]string),
		services:    make(map[string][][2]string),
		epgEvents:   make(map[string][]EPGEvent),
	}

	dt.populateDefaultData()

	mux := http.NewServeMux()
	// OpenWebIF API
	mux.HandleFunc("/api/about", dt.handleAbout)
	mux.HandleFunc("/api/statusinfo", dt.handleStatusInfo)
	mux.HandleFunc("/api/getcurrent", dt.handleGetCurrent)
	mux.HandleFunc("/api/signal", dt.handleSignal)
	mux.HandleFunc("/api/zap", dt.handleZap)
	mux.HandleFunc("/api/bouquets", dt.handleBouquets)
	mux.HandleFunc("/api/getallservices", dt.handleAllServices)
	mux.HandleFunc("/api/getservices", dt.handleServices)
	mux.HandleFunc("/api/epgservice", dt.handleEPG)

	// Streaming endpoints
	mux.HandleFunc("/web/stream.m3u", dt.handleWebStreamM3U)

	// Catch-all for direct TS stream: /<sRef> or /stream/<sRef>
	mux.HandleFunc("/", dt.handleStreamOrFallback)

	dt.server = httptest.NewServer(mux)
	return dt
}

// NewVuUno4KTwin creates a Digital Twin with test cleanup registered automatically.
func NewVuUno4KTwin(t testing.TB, opts ...DigitalTwinOption) *DigitalTwin {
	dt := NewDigitalTwin(opts...)
	t.Cleanup(dt.Close)
	return dt
}

// Close shuts down the underlying HTTP server.
func (dt *DigitalTwin) Close() {
	if dt.server != nil {
		dt.server.Close()
	}
}

// URL returns the base HTTP URL of the digital twin.
func (dt *DigitalTwin) URL() string {
	if dt.server == nil {
		return ""
	}
	return dt.server.URL
}

// StreamURL returns the direct TS stream URL for a given service reference.
func (dt *DigitalTwin) StreamURL(serviceRef string) string {
	return fmt.Sprintf("%s/%s", dt.URL(), serviceRef)
}

// ParseTransponderKey extracts the DVB transponder identification (TSID:ONID:Namespace) from a service reference.
// ServiceRef format: type:flags:serviceType:serviceId:tsid:onid:dvbNamespace:parentTsid:parentOnid:unused:
func ParseTransponderKey(serviceRef string) string {
	parts := strings.Split(strings.Trim(serviceRef, ":"), ":")
	if len(parts) >= 7 {
		tsid := parts[4]
		onid := parts[5]
		namespace := parts[6]
		return fmt.Sprintf("%s:%s:%s", tsid, onid, namespace)
	}
	return serviceRef
}

func (dt *DigitalTwin) populateDefaultData() {
	dt.bouquets = map[string]string{
		"1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.favourites.tv\" ORDER BY bouquet": "Favourites (TV)",
		"1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.hd.tv\" ORDER BY bouquet":         "HD Channels",
	}

	// Two services on Transponder 1 (3FB:1:C00000)
	dt.services["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.hd.tv\" ORDER BY bouquet"] = [][2]string{
		{"1:0:19:283D:3FB:1:C00000:0:0:0:", "Das Erste HD"},
		{"1:0:19:283E:3FB:1:C00000:0:0:0:", "ZDF HD"},
		// Service on Transponder 2 (44D:1:C00000)
		{"1:0:1:6DCA:44D:1:C00000:0:0:0:", "RTL Television"},
		// Service on Transponder 3 (453:1:C00000)
		{"1:0:1:6DCB:453:1:C00000:0:0:0:", "ProSieben"},
	}

	dt.currentServiceRef = "1:0:19:283D:3FB:1:C00000:0:0:0:"
}

// acquireTuner locks a physical tuner for the given serviceRef/transponder.
func (dt *DigitalTwin) acquireTuner(serviceRef string) (int, error) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	tpKey := ParseTransponderKey(serviceRef)

	// Case 1: Transponder already active -> share existing tuner
	if alloc, ok := dt.allocations[tpKey]; ok {
		// Enforce hardware demuxer limit per physical tuner
		if dt.config.DemuxersPerTuner > 0 && len(alloc.ActiveStreams) >= dt.config.DemuxersPerTuner {
			if _, exists := alloc.ActiveStreams[serviceRef]; !exists {
				if dt.config.CrashOnTunerExhaustion {
					dt.assertionFailed = true
					msg := fmt.Sprintf("%s (Demuxer exhaustion on tuner %d: %d concurrent services on transponder %s)",
						OpenATVAssertionMessage, alloc.PhysicalTunerID, len(alloc.ActiveStreams), tpKey)
					dt.assertionLog = append(dt.assertionLog, msg)
					return -1, fmt.Errorf("CRASH: %s", msg)
				}
				return -1, fmt.Errorf("demuxer exhaustion: tuner %d reached limit of %d concurrent services", alloc.PhysicalTunerID, dt.config.DemuxersPerTuner)
			}
		}
		alloc.ActiveStreams[serviceRef]++
		dt.totalStreams.Add(1)
		return alloc.PhysicalTunerID, nil
	}

	// Case 2: New transponder -> allocate new physical tuner
	freeSlot := -1
	for i, inUse := range dt.tunerSlots {
		if !inUse {
			freeSlot = i
			break
		}
	}

	if freeSlot == -1 {
		// Tuner exhaustion!
		if dt.config.CrashOnTunerExhaustion {
			dt.assertionFailed = true
			msg := fmt.Sprintf("%s (Attempted service: %s, active transponders: %d, max tuners: %d)",
				OpenATVAssertionMessage, serviceRef, len(dt.allocations), dt.config.PhysicalTuners)
			dt.assertionLog = append(dt.assertionLog, msg)
			return -1, fmt.Errorf("CRASH: %s", msg)
		}
		return -1, fmt.Errorf("tuner exhaustion: all %d tuners are busy", dt.config.PhysicalTuners)
	}

	dt.tunerSlots[freeSlot] = true
	dt.allocations[tpKey] = &TunerAllocation{
		PhysicalTunerID: freeSlot,
		TransponderKey:  tpKey,
		ActiveStreams:   map[string]int{serviceRef: 1},
	}
	dt.totalStreams.Add(1)

	currentActive := len(dt.allocations)
	if currentActive > dt.peakTunersUsed {
		dt.peakTunersUsed = currentActive
	}

	return freeSlot, nil
}

// releaseTuner releases a stream's hold on a tuner.
func (dt *DigitalTwin) releaseTuner(serviceRef string) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	tpKey := ParseTransponderKey(serviceRef)
	alloc, ok := dt.allocations[tpKey]
	if !ok {
		return
	}

	alloc.ActiveStreams[serviceRef]--
	if alloc.ActiveStreams[serviceRef] <= 0 {
		delete(alloc.ActiveStreams, serviceRef)
	}

	// If no more streams on this transponder, release physical tuner
	if len(alloc.ActiveStreams) == 0 {
		dt.tunerSlots[alloc.PhysicalTunerID] = false
		delete(dt.allocations, tpKey)
	}
}

// Handlers

func (dt *DigitalTwin) handleAbout(w http.ResponseWriter, _ *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	tuners := make([]map[string]string, dt.config.PhysicalTuners)
	for i := 0; i < dt.config.PhysicalTuners; i++ {
		tuners[i] = map[string]string{
			"name": fmt.Sprintf("Tuner %c: DVB-S2 FBC", 'A'+i),
			"type": "DVB-S2",
		}
	}

	resp := map[string]any{
		"info": map[string]any{
			"model":        dt.config.Model,
			"brand":        dt.config.Brand,
			"boxtype":      dt.config.BoxType,
			"enigmaver":    dt.config.EnigmaVersion,
			"webifver":     dt.config.WebIFVersion,
			"imagever":     dt.config.ImageVersion,
			"kernelver":    dt.config.KernelVersion,
			"tuners":       tuners,
			"tuners_count": dt.config.PhysicalTuners,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleStatusInfo(w http.ResponseWriter, _ *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	isStreaming := "false"
	if len(dt.allocations) > 0 {
		isStreaming = "true"
	}

	resp := map[string]any{
		"result":                 true,
		"inStandby":              strconv.FormatBool(dt.isStandby),
		"isRecording":            "false",
		"isStreaming":            isStreaming,
		"currservice_name":       "Twin Active Channel",
		"currservice_serviceref": dt.currentServiceRef,
		"tuners_in_use":          len(dt.allocations),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleGetCurrent(w http.ResponseWriter, _ *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	vpid := 0
	apid := 0
	pmtpid := 0
	if dt.isLocked && dt.hasPIDs {
		vpid = pidVideo
		apid = pidAudio
		pmtpid = pidPMT
	}

	resp := map[string]any{
		"result": true,
		"info": map[string]any{
			"serviceref": dt.currentServiceRef,
			"name":       "Twin Active Channel",
			"vpid":       strconv.Itoa(vpid),
			"apid":       strconv.Itoa(apid),
			"pmtpid":     strconv.Itoa(pmtpid),
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleSignal(w http.ResponseWriter, _ *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	resp := map[string]any{
		"result":    true,
		"snr":       dt.snrSignal,
		"agc":       80,
		"ber":       0,
		"inStandby": strconv.FormatBool(dt.isStandby),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleZap(w http.ResponseWriter, r *http.Request) {
	sRef := r.URL.Query().Get("sRef")
	if sRef == "" {
		http.Error(w, "Missing sRef parameter", http.StatusBadRequest)
		return
	}

	dt.mu.Lock()
	dt.currentServiceRef = sRef
	dt.totalZaps.Add(1)
	dt.mu.Unlock()

	resp := map[string]any{
		"result":  true,
		"message": "Channel switched successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleBouquets(w http.ResponseWriter, _ *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	bouquets := make([][]string, 0, len(dt.bouquets))
	for ref, name := range dt.bouquets {
		bouquets = append(bouquets, []string{ref, name})
	}
	resp := map[string]any{"bouquets": bouquets}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleAllServices(w http.ResponseWriter, r *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	bouquetRef := r.URL.Query().Get("sRef")
	services, ok := dt.services[bouquetRef]
	if !ok {
		services = [][2]string{}
	}

	servicesList := make([]map[string]any, len(services))
	for i, svc := range services {
		servicesList[i] = map[string]any{
			"servicereference": svc[0],
			"servicename":      svc[1],
		}
	}
	resp := map[string]any{"services": servicesList}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleServices(w http.ResponseWriter, r *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	bouquetRef := r.URL.Query().Get("sRef")
	services, ok := dt.services[bouquetRef]
	if !ok {
		services = [][2]string{}
	}

	servicesList := make([]map[string]any, len(services))
	for i, svc := range services {
		servicesList[i] = map[string]any{
			"servicereference": svc[0],
			"servicename":      svc[1],
		}
	}
	resp := ServicesResponse{Services: servicesList}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleEPG(w http.ResponseWriter, r *http.Request) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	sRef := r.URL.Query().Get("sRef")
	events, ok := dt.epgEvents[sRef]
	if !ok {
		events = []EPGEvent{}
	}
	resp := EPGResponse{Events: events}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (dt *DigitalTwin) handleWebStreamM3U(w http.ResponseWriter, r *http.Request) {
	sRef := r.URL.Query().Get("ref")
	name := r.URL.Query().Get("name")
	if sRef == "" {
		http.Error(w, "Missing ref parameter", http.StatusBadRequest)
		return
	}

	m3uContent := fmt.Sprintf("#EXTM3U\n#EXTINF:-1,%s\n%s/%s\n", name, dt.URL(), sRef)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(m3uContent))
}

func (dt *DigitalTwin) handleStreamOrFallback(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	path = strings.TrimPrefix(path, "stream/")

	// If path is a service reference (starts with 1: or contains DVB ref delimiters)
	if strings.HasPrefix(path, "1:") || strings.Contains(path, ":") {
		dt.serveTSStream(w, r, path)
		return
	}

	http.NotFound(w, r)
}

func (dt *DigitalTwin) serveTSStream(w http.ResponseWriter, r *http.Request, sRef string) {
	tunerID, err := dt.acquireTuner(sRef)
	if err != nil {
		if dt.config.CrashOnTunerExhaustion {
			http.Error(w, fmt.Sprintf("503 Service Unavailable: %v", err), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "503 Service Unavailable: All tuners in use", http.StatusServiceUnavailable)
		return
	}
	defer dt.releaseTuner(sRef)

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("X-Tuner-ID", strconv.Itoa(tunerID))
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	_ = StreamTSContinuously(r.Context(), w, dt.config.MaxStreamBatches, dt.config.CAMGraceWindow)
}

// Assertions & Forensic Methods for Tests

// AssertNoAssertionFailure fails the test if an OpenATV tuner assertion crash was simulated.
func (dt *DigitalTwin) AssertNoAssertionFailure(t testing.TB) {
	t.Helper()
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	if dt.assertionFailed {
		t.Fatalf("🚨 OpenATV Receiver Assertion Failure Detected:\n%s", strings.Join(dt.assertionLog, "\n"))
	}
}

// AssertTunerCrashOccurred verifies that an assertion crash was intentionally triggered.
func (dt *DigitalTwin) AssertTunerCrashOccurred(t testing.TB) {
	t.Helper()
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	if !dt.assertionFailed {
		t.Fatalf("Expected OpenATV Receiver Assertion Failure, but none occurred (Peak tuners: %d)", dt.peakTunersUsed)
	}
}

// ActiveTunersCount returns the number of physical tuners currently in use.
func (dt *DigitalTwin) ActiveTunersCount() int {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return len(dt.allocations)
}

// PeakTunersUsed returns the peak number of physical tuners used concurrently.
func (dt *DigitalTwin) PeakTunersUsed() int {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return dt.peakTunersUsed
}

// TotalStreamsStarted returns the total number of streams initiated.
func (dt *DigitalTwin) TotalStreamsStarted() int64 {
	return dt.totalStreams.Load()
}
