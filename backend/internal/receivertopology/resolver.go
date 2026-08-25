// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// TransponderResolver resolves an authoritative MultiplexID with complete physical RF tuning parameters for a service reference.
type TransponderResolver interface {
	ResolveTransponder(ctx context.Context, serviceRef string) (MultiplexID, error)
}

// LamedbSource provides the receiver's own service database, which is the
// authoritative record of which physical carrier each transport stream lives on.
type LamedbSource interface {
	GetLamedb(ctx context.Context) ([]byte, error)
}

// ErrLamedbUnavailable indicates the receiver's service database could not be
// fetched or understood, so no authoritative RF facts are available.
var ErrLamedbUnavailable = errors.New("receiver service database unavailable")

// defaultLamedbCooldown bounds how often a resolution miss may re-fetch the service
// database. A miss means the receiver has tuned a carrier this process has not seen,
// which happens after a channel scan - worth re-reading for, but not once per request.
const defaultLamedbCooldown = 60 * time.Second

// TransponderRegistry maintains an in-memory database of authoritative physical RF tuning identities.
//
// Every fact in it originates from the receiver: either registered directly from an
// observed tune, or read from the receiver's service database. There is deliberately
// no table of hand-maintained broadcast frequencies - one existed here previously and
// every entry in it was wrong, which is worse than having none, because a confidently
// wrong RF plane makes the allocator believe two services share a tuner when they do not.
type TransponderRegistry struct {
	mu                     sync.RWMutex
	discoveredTransponders map[string]TransponderKey // Keyed by TSID:ONID:Namespace
	discoveredServices     map[string]MultiplexID    // Keyed by canonical serviceRef
	lamedb                 LamedbSource
	lastLoad               time.Time
	cooldown               time.Duration
	now                    func() time.Time

	// loadMu serializes service database fetches so a burst of misses produces one
	// request rather than one per caller, and is never held together with mu across I/O.
	loadMu sync.Mutex
}

// NewTransponderRegistry creates a new in-memory transponder registry.
func NewTransponderRegistry() *TransponderRegistry {
	return &TransponderRegistry{
		discoveredTransponders: make(map[string]TransponderKey),
		discoveredServices:     make(map[string]MultiplexID),
		cooldown:               defaultLamedbCooldown,
		now:                    time.Now,
	}
}

// SetLamedbSource configures the receiver client used to read the authoritative
// service database.
func (r *TransponderRegistry) SetLamedbSource(src LamedbSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lamedb = src
}

// RegisterTransponder stores authoritative RF tuning parameters for a physical transport stream.
func (r *TransponderRegistry) RegisterTransponder(tsid uint16, onid uint16, namespace uint32, tpKey TransponderKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discoveredTransponders[transponderLookupKey(tsid, onid, namespace)] = tpKey
}

// RegisterService stores an authoritative MultiplexID directly for a service reference.
func (r *TransponderRegistry) RegisterService(serviceRef string, mux MultiplexID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discoveredServices[serviceRef] = mux
}

// LoadLamedb reads the receiver's service database and registers every transponder in it.
//
// It returns the parsed snapshot so a caller can log what the receiver actually
// knows: a database that parses but yields nothing usable is a configuration
// problem worth reporting, not a silent success.
func (r *TransponderRegistry) LoadLamedb(ctx context.Context) (LamedbSnapshot, error) {
	r.mu.RLock()
	src := r.lamedb
	r.mu.RUnlock()
	if src == nil {
		return LamedbSnapshot{}, fmt.Errorf("%w: no source configured", ErrLamedbUnavailable)
	}

	r.loadMu.Lock()
	defer r.loadMu.Unlock()

	data, err := src.GetLamedb(ctx)
	if err != nil {
		return LamedbSnapshot{}, fmt.Errorf("%w: %v", ErrLamedbUnavailable, err)
	}

	return r.LoadLamedbBytes(data)
}

// LoadLamedbBytes registers every transponder in an already-fetched service database.
//
// Separate from LoadLamedb so the same facts can be supplied from a file an operator
// copied off the receiver, and so the parse step is exercisable without a receiver.
func (r *TransponderRegistry) LoadLamedbBytes(data []byte) (LamedbSnapshot, error) {
	snap, err := ParseLamedb(data)
	if err != nil {
		return LamedbSnapshot{}, fmt.Errorf("%w: %v", ErrLamedbUnavailable, err)
	}

	// Stamp the attempt regardless of yield: a database that parsed to nothing must
	// not be retried on every subsequent miss.
	r.mu.Lock()
	r.lastLoad = r.now()
	for _, tp := range snap.Transponders {
		r.discoveredTransponders[transponderLookupKey(tp.TSID, tp.ONID, tp.DVBNamespace)] = tp.Key
	}
	r.mu.Unlock()

	return snap, nil
}

// ResolveTransponder resolves authoritative physical RF tuning parameters using the strict hierarchy:
//  1. Live discovered service cache (an observed tune, the strongest evidence there is)
//  2. Live discovered transponder cache (populated by observed tunes and the service database)
//  3. A cooldown-bounded re-read of the receiver's service database, for carriers
//     added since this process last looked
func (r *TransponderRegistry) ResolveTransponder(ctx context.Context, serviceRef string) (MultiplexID, error) {
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

	lookupKey := transponderLookupKey(baseMux.TSID, baseMux.ONID, baseMux.DVBNamespace)

	r.mu.RLock()
	tpKey, hasDiscovered := r.discoveredTransponders[lookupKey]
	r.mu.RUnlock()

	if hasDiscovered {
		return r.enrichMultiplex(baseMux, tpKey), nil
	}

	// A miss for a service reference the receiver produced means our copy of the
	// service database predates it, so re-read once and try again.
	if r.refreshDue() {
		if _, lErr := r.LoadLamedb(ctx); lErr == nil {
			r.mu.RLock()
			tpKey, hasDiscovered = r.discoveredTransponders[lookupKey]
			r.mu.RUnlock()
			if hasDiscovered {
				return r.enrichMultiplex(baseMux, tpKey), nil
			}
		}
	}

	return MultiplexID{}, fmt.Errorf("%w: missing RF parameters for TSID 0x%04X ONID 0x%04X Namespace 0x%08X",
		ErrAuthoritativeTransponderUnavailable, baseMux.TSID, baseMux.ONID, baseMux.DVBNamespace)
}

// refreshDue reports whether a service database re-read is permitted right now.
func (r *TransponderRegistry) refreshDue() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.lamedb == nil {
		return false
	}
	return r.lastLoad.IsZero() || r.now().Sub(r.lastLoad) >= r.cooldown
}

func transponderLookupKey(tsid uint16, onid uint16, namespace uint32) string {
	return fmt.Sprintf("%04X:%04X:%08X", tsid, onid, namespace)
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
