// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import (
	"strings"
	"sync"
	"time"
)

// ScrambleScope says whether an encrypted upstream is a property of the one
// service that was asked for, or of the receiver as a whole.
//
// The two look identical at the packet level — every packet carries the
// scrambling bits either way — but they mean opposite things to whoever has to
// act. One service scrambled while others come through clear is an entitlement
// question. Nothing coming through clear at all is a receiver that has stopped
// descrambling, and no amount of retrying, profile switching or channel hopping
// will change it. That case was observed here: two different services on the same
// transponder read 86% and 93% scrambled within half an hour, and both played
// perfectly after the receiver was restarted.
type ScrambleScope string

const (
	// ScrambleScopeUnknown means there is not enough evidence to attribute it.
	ScrambleScopeUnknown ScrambleScope = ""
	// ScrambleScopeService attributes it to the requested service.
	ScrambleScopeService ScrambleScope = "service"
	// ScrambleScopeReceiver attributes it to the receiver's descrambling.
	ScrambleScopeReceiver ScrambleScope = "receiver"
)

// DetailDescramblerDown is the operator-facing text for a receiver-wide fault.
const DetailDescramblerDown = "receiver is not descrambling any service (check the CAM or softcam on the receiver)"

const (
	defaultScrambleObserveWindow = 15 * time.Minute
	defaultScrambleMinServices   = 2
)

type scrambleObservation struct {
	scrambled bool
	at        time.Time
}

// ScrambleObserver attributes scrambled upstreams by remembering, for a short
// window, which services came through clear and which did not.
//
// Deliberately conservative: a single clear observation anywhere in the window
// proves descrambling works, and the verdict falls back to the service. Blaming
// the receiver requires several distinct services to have failed with nothing
// succeeding, because telling someone their receiver is broken when it is not is
// worse than saying nothing.
//
// Safe for concurrent use.
type ScrambleObserver struct {
	mu          sync.Mutex
	window      time.Duration
	minServices int
	seen        map[string]scrambleObservation
}

// NewScrambleObserver builds an observer. Zero values select the defaults.
func NewScrambleObserver(window time.Duration, minServices int) *ScrambleObserver {
	if window <= 0 {
		window = defaultScrambleObserveWindow
	}
	if minServices <= 0 {
		minServices = defaultScrambleMinServices
	}
	return &ScrambleObserver{
		window:      window,
		minServices: minServices,
		seen:        make(map[string]scrambleObservation),
	}
}

// Observe records the descrambling outcome for one service. Only the most recent
// outcome per service counts, so a service that recovers stops arguing for a
// receiver fault.
func (o *ScrambleObserver) Observe(serviceRef string, scrambled bool, at time.Time) {
	ref := strings.TrimSpace(serviceRef)
	if o == nil || ref == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen[ref] = scrambleObservation{scrambled: scrambled, at: at}
	o.pruneLocked(at)
}

// Scope attributes the current scrambled condition.
func (o *ScrambleObserver) Scope(now time.Time) ScrambleScope {
	if o == nil {
		return ScrambleScopeUnknown
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pruneLocked(now)

	scrambled := 0
	for _, obs := range o.seen {
		if !obs.scrambled {
			// Something descrambled inside the window: the receiver works, so
			// whatever else failed is about that service.
			return ScrambleScopeService
		}
		scrambled++
	}
	if scrambled >= o.minServices {
		return ScrambleScopeReceiver
	}
	return ScrambleScopeService
}

func (o *ScrambleObserver) pruneLocked(now time.Time) {
	for ref, obs := range o.seen {
		if now.Sub(obs.at) > o.window {
			delete(o.seen, ref)
		}
	}
}
