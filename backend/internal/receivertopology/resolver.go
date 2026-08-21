// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

// TransponderResolver resolves an authoritative MultiplexID with complete physical RF tuning parameters for a service reference.
type TransponderResolver interface {
	ResolveTransponder(ctx context.Context, serviceRef string) (MultiplexID, error)
}

// ChannelInfoDiscoverer fetches live physical tuning details from an external receiver.
type ChannelInfoDiscoverer interface {
	GetChannelInfo(ctx context.Context, serviceRef string) (*openwebif.ChannelInfoResponse, error)
}

// TransponderRegistry maintains an in-memory database of authoritative physical RF tuning identities.
type TransponderRegistry struct {
	mu                         sync.RWMutex
	discoveredTransponders     map[string]TransponderKey // Keyed by TSID:ONID:Namespace (populated by live receiver discovery)
	discoveredServices         map[string]MultiplexID    // Keyed by canonical serviceRef (populated by live receiver discovery)
	staticFallbackTransponders map[string]TransponderKey // Keyed by TSID:ONID:Namespace (verified static fallback)
	discoverer                 ChannelInfoDiscoverer     // Live OpenWebIF discovery client
}

// NewTransponderRegistry creates a new in-memory transponder registry.
func NewTransponderRegistry() *TransponderRegistry {
	return &TransponderRegistry{
		discoveredTransponders:     make(map[string]TransponderKey),
		discoveredServices:         make(map[string]MultiplexID),
		staticFallbackTransponders: make(map[string]TransponderKey),
	}
}

// SetDiscoverer configures an OpenWebIF discovery client to query live RF tuning facts.
func (r *TransponderRegistry) SetDiscoverer(d ChannelInfoDiscoverer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discoverer = d
}

// RegisterTransponder stores the authoritative RF tuning parameters in the live discovered transponder cache.
func (r *TransponderRegistry) RegisterTransponder(tsid uint16, onid uint16, namespace uint32, tpKey TransponderKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%04X:%04X:%08X", tsid, onid, namespace)
	r.discoveredTransponders[key] = tpKey
}

// RegisterStaticFallback stores verified static fallback parameters for a physical transport stream / multiplex.
func (r *TransponderRegistry) RegisterStaticFallback(tsid uint16, onid uint16, namespace uint32, tpKey TransponderKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%04X:%04X:%08X", tsid, onid, namespace)
	r.staticFallbackTransponders[key] = tpKey
}

// RegisterService stores an authoritative MultiplexID directly for a service reference.
func (r *TransponderRegistry) RegisterService(serviceRef string, mux MultiplexID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discoveredServices[serviceRef] = mux
}

// ResolveTransponder resolves authoritative physical RF tuning parameters using the strict hierarchy:
// 1. Live Discovered In-Memory Cache
// 2. Authoritative Live Receiver Query (OpenWebIF /api/channelinfo)
// 3. Verified Static Fallback (if live receiver is unreachable or undiscoverer configured)
func (r *TransponderRegistry) ResolveTransponder(ctx context.Context, serviceRef string) (MultiplexID, error) {
	// Step 1: Check Live Discovered Service Cache
	r.mu.RLock()
	if mux, ok := r.discoveredServices[serviceRef]; ok {
		r.mu.RUnlock()
		return mux, nil
	}
	r.mu.RUnlock()

	baseMux, err := ParseServiceRef(serviceRef)
	if err != nil {
		return MultiplexID{}, err
	}

	lookupKey := fmt.Sprintf("%04X:%04X:%08X", baseMux.TSID, baseMux.ONID, baseMux.DVBNamespace)

	// Step 2: Check Live Discovered Transponder Cache
	r.mu.RLock()
	tpKey, hasDiscovered := r.discoveredTransponders[lookupKey]
	discoverer := r.discoverer
	r.mu.RUnlock()

	if hasDiscovered {
		return r.enrichMultiplex(baseMux, tpKey), nil
	}

	// Step 3: Authoritative Live Receiver Query (OpenWebIF)
	if discoverer != nil {
		info, dErr := discoverer.GetChannelInfo(ctx, serviceRef)
		if dErr == nil && info != nil && info.Result && info.Service.Transponder.FrequencyHz > 0 {
			tp := info.Service.Transponder
			freqHz := tp.FrequencyHz
			if freqHz < 100000000 { // If returned in kHz (e.g. 11273000 -> 11273000000)
				freqHz = freqHz * 1000
			}
			pol := PolarizationHorizontal
			if strings.EqualFold(tp.Polarization, "V") || strings.EqualFold(tp.Polarization, "1") {
				pol = PolarizationVertical
			}
			sys := DeliverySystemDVBS2
			if strings.Contains(strings.ToUpper(tp.System), "DVB-S2") {
				sys = DeliverySystemDVBS2
			} else if strings.Contains(strings.ToUpper(tp.System), "DVB-S") {
				sys = DeliverySystemDVBS
			} else if strings.Contains(strings.ToUpper(tp.System), "DVB-C") {
				sys = DeliverySystemDVBC
			} else if strings.Contains(strings.ToUpper(tp.System), "DVB-T2") {
				sys = DeliverySystemDVBT2
			} else if strings.Contains(strings.ToUpper(tp.System), "DVB-T") {
				sys = DeliverySystemDVBT
			}

			discoveredKey := TransponderKey{
				DeliverySystem:  sys,
				OrbitalPosition: tp.OrbitalPosition,
				FrequencyHz:     freqHz,
				Polarization:    pol,
				StreamID:        tp.StreamID,
				PLSMode:         PLSMode(tp.PLSMode),
				PLSCode:         tp.PLSCode,
			}

			r.RegisterTransponder(baseMux.TSID, baseMux.ONID, baseMux.DVBNamespace, discoveredKey)
			return r.enrichMultiplex(baseMux, discoveredKey), nil
		}
	}

	// Step 4: Verified Static Fallback (Only explicit, verified standard transponders)
	r.mu.RLock()
	fallbackKey, hasFallback := r.staticFallbackTransponders[lookupKey]
	r.mu.RUnlock()

	if hasFallback {
		return r.enrichMultiplex(baseMux, fallbackKey), nil
	}

	return MultiplexID{}, fmt.Errorf("%w: missing RF parameters for TSID 0x%04X ONID 0x%04X Namespace 0x%08X", ErrAuthoritativeTransponderUnavailable, baseMux.TSID, baseMux.ONID, baseMux.DVBNamespace)
}

func (r *TransponderRegistry) enrichMultiplex(baseMux MultiplexID, tpKey TransponderKey) MultiplexID {
	baseMux.TransponderKey = &tpKey
	if baseMux.DVBType == DVBTypeSat {
		band := BandHigh
		if tpKey.FrequencyHz > 0 && tpKey.FrequencyHz < 11700000000 {
			band = BandLow
		}
		pol := tpKey.Polarization
		if pol == "" {
			pol = PolarizationHorizontal
		}
		baseMux.RFPlane = &RFPlane{
			SatPosition:  SatellitePosition(tpKey.OrbitalPosition),
			Band:         band,
			Polarization: pol,
		}
	}
	return baseMux
}

// PopulateStandardTransponderTables enriches the registry with verified static fallback RF tuning parameters
// for standard European satellite broadcast networks (Astra 19.2°E, Hotbird 13.0°E).
func PopulateStandardTransponderTables(r *TransponderRegistry) {
	const astra192 = 0x00C00000
	const hotbird130 = 0x00820000

	// Verified Astra 19.2°E Transponders (TSID, ONID, Namespace -> TransponderKey)
	astraTPs := []struct {
		tsid uint16
		onid uint16
		key  TransponderKey
	}{
		// ORF Digital (ORF 1 HD, ORF 2 HD, ORF III HD, ORF Sport+ HD)
		{0x03EF, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11273000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// ARD Digital 1 (Das Erste HD, SWR HD, arte HD)
		{0x03FB, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11494000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// ZDF Digital (ZDF HD, zdf_neo HD)
		{0x03F3, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11362000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// ZDF Digital 2 (3sat HD, KiKa HD, ZDFinfo HD)
		{0x044D, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11347000000, Polarization: PolarizationVertical, StreamID: -1}},
		// ARD Digital 2 (BR HD, NDR HD, Phoenix HD)
		{0x041F, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11582000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// ARD Digital 3 (WDR HD Köln, WDR HD Regional)
		{0x0453, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 12422000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// RTL Group Deutschland SD (RTL, VOX, Super RTL, n-tv, Nitro)
		{0x041B, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS, OrbitalPosition: 192, FrequencyHz: 12188000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// ProSiebenSat.1 Digital SD (ProSieben, SAT.1, Kabel 1, sixx, Pro7 MAXX)
		{0x0441, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS, OrbitalPosition: 192, FrequencyHz: 12544000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// ServusTV / Red Bull HD
		{0x0007, 0x0001, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11303000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// Sky Deutschland Transponder 1
		{0x0011, 0x0085, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11992000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// Sky Deutschland Transponder 2
		{0x000C, 0x0085, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 192, FrequencyHz: 11758000000, Polarization: PolarizationHorizontal, StreamID: -1}},
	}

	for _, tp := range astraTPs {
		r.RegisterStaticFallback(tp.tsid, tp.onid, astra192, tp.key)
	}

	// Verified Hotbird 13.0°E Transponders
	hotbirdTPs := []struct {
		tsid uint16
		onid uint16
		key  TransponderKey
	}{
		// SRG SSR (SRF 1 HD, SRF zwei HD, RTS 1 HD, RSI LA 1 HD)
		{0x2134, 0x013E, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 130, FrequencyHz: 10971000000, Polarization: PolarizationHorizontal, StreamID: -1}},
		// Rai HD (Rai 1 HD, Rai 2 HD, Rai 3 HD)
		{0x01A4, 0x013E, TransponderKey{DeliverySystem: DeliverySystemDVBS2, OrbitalPosition: 130, FrequencyHz: 11766000000, Polarization: PolarizationVertical, StreamID: -1}},
	}

	for _, tp := range hotbirdTPs {
		r.RegisterStaticFallback(tp.tsid, tp.onid, hotbird130, tp.key)
	}
}
