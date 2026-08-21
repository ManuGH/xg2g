// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"context"
	"fmt"
	"sync"
)

// TransponderResolver resolves an authoritative MultiplexID with complete physical RF tuning parameters for a service reference.
type TransponderResolver interface {
	ResolveTransponder(ctx context.Context, serviceRef string) (MultiplexID, error)
}

// TransponderRegistry maintains an in-memory database of authoritative physical RF tuning identities.
type TransponderRegistry struct {
	mu           sync.RWMutex
	transponders map[string]TransponderKey // Keyed by canonical TSID:ONID:Namespace
	services     map[string]MultiplexID    // Keyed by canonical service reference
}

// NewTransponderRegistry creates a new in-memory transponder registry.
func NewTransponderRegistry() *TransponderRegistry {
	return &TransponderRegistry{
		transponders: make(map[string]TransponderKey),
		services:     make(map[string]MultiplexID),
	}
}

// RegisterTransponder stores the authoritative RF tuning parameters for a physical transport stream / multiplex.
func (r *TransponderRegistry) RegisterTransponder(tsid uint16, onid uint16, namespace uint32, tpKey TransponderKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%04X:%04X:%08X", tsid, onid, namespace)
	r.transponders[key] = tpKey
}

// RegisterService stores an authoritative MultiplexID directly for a service reference.
func (r *TransponderRegistry) RegisterService(serviceRef string, mux MultiplexID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[serviceRef] = mux
}

// ResolveTransponder extracts the base multiplex from serviceRef and enriches it with authoritative RF tuning parameters.
func (r *TransponderRegistry) ResolveTransponder(ctx context.Context, serviceRef string) (MultiplexID, error) {
	r.mu.RLock()
	if mux, ok := r.services[serviceRef]; ok {
		r.mu.RUnlock()
		return mux, nil
	}
	r.mu.RUnlock()

	baseMux, err := ParseServiceRef(serviceRef)
	if err != nil {
		return MultiplexID{}, err
	}

	r.mu.RLock()
	lookupKey := fmt.Sprintf("%04X:%04X:%08X", baseMux.TSID, baseMux.ONID, baseMux.DVBNamespace)
	tpKey, hasTP := r.transponders[lookupKey]
	r.mu.RUnlock()

	if hasTP {
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
	}

	return baseMux, nil
}
