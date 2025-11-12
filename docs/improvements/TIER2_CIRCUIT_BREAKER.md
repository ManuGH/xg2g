# Tier 2: Circuit Breaker for Stream Detection

## Ziel
Robuste Fehlerbehandlung für wiederholte Stream Detection Failures

## Problem
Aktuell: Wenn Port 8001/17999 wiederholt fehlschlägt:
- Jede Request versucht erneut → Latency
- Keine automatische Fallback-Strategie
- Circuit bleibt offen trotz permanentem Fehler

## Lösung: Circuit Breaker Pattern

### States:
- **Closed**: Normal operation, alle Requests durchlassen
- **Open**: Nach X Fehlern, alle Requests sofort abweisen (fail-fast)
- **Half-Open**: Nach Timeout, einzelne Test-Requests durchlassen

### State Transitions:
```
Closed → Open: Nach 5 Fehlern in 10s
Open → Half-Open: Nach 30s Timeout
Half-Open → Closed: Nach 3 erfolgreichen Requests
Half-Open → Open: Bei erneutem Fehler
```

---

## Implementation

### 1. Circuit Breaker Package

**Datei:** `internal/circuitbreaker/breaker.go` (neu)

```go
// SPDX-License-Identifier: MIT

package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

var (
	circuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "xg2g",
			Name:      "circuit_breaker_state",
			Help:      "Circuit breaker state (0=closed, 1=open, 2=half_open)",
		},
		[]string{"name"},
	)

	circuitBreakerTrips = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "xg2g",
			Name:      "circuit_breaker_trips_total",
			Help:      "Total circuit breaker trips (transitions to open)",
		},
		[]string{"name"},
	)
)

// Config holds circuit breaker configuration
type Config struct {
	Name             string        // Name for metrics/logging
	FailureThreshold int           // Failures to trip open
	SuccessThreshold int           // Successes to close from half-open
	Timeout          time.Duration // How long to stay open
	WindowSize       time.Duration // Rolling window for failure counting
}

// DefaultConfig returns sensible defaults
func DefaultConfig(name string) Config {
	return Config{
		Name:             name,
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          30 * time.Second,
		WindowSize:       10 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config Config

	state            State
	failures         int
	successes        int
	lastFailureTime  time.Time
	lastStateChange  time.Time
	consecutiveFails []time.Time // For windowed failure counting

	mu sync.RWMutex
}

// New creates a new circuit breaker
func New(config Config) *CircuitBreaker {
	cb := &CircuitBreaker{
		config:           config,
		state:            StateClosed,
		consecutiveFails: make([]time.Time, 0),
		lastStateChange:  time.Now(),
	}

	// Initialize metrics
	circuitBreakerState.WithLabelValues(config.Name).Set(0)

	return cb
}

// Call executes the given function with circuit breaker protection
func (cb *CircuitBreaker) Call(ctx context.Context, fn func() error) error {
	// Check if we can proceed
	if err := cb.beforeCall(); err != nil {
		return err
	}

	// Execute function
	err := fn()

	// Record result
	cb.afterCall(err)

	return err
}

// beforeCall checks if the call can proceed
func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case StateOpen:
		// Check if timeout expired
		if time.Since(cb.lastStateChange) >= cb.config.Timeout {
			// Transition to half-open in afterCall
			return nil
		}
		return fmt.Errorf("circuit breaker %s is open", cb.config.Name)

	case StateClosed, StateHalfOpen:
		return nil

	default:
		return fmt.Errorf("unknown circuit breaker state: %v", cb.state)
	}
}

// afterCall records the result and updates state
func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}
}

// onFailure handles a failed call
func (cb *CircuitBreaker) onFailure() {
	cb.lastFailureTime = time.Now()
	cb.consecutiveFails = append(cb.consecutiveFails, time.Now())

	// Clean old failures outside window
	cb.cleanOldFailures()

	switch cb.state {
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		// Any failure in half-open → back to open
		cb.transitionTo(StateOpen)
	}
}

// onSuccess handles a successful call
func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		// Reset failure count on success
		cb.failures = 0
		cb.consecutiveFails = make([]time.Time, 0)

	case StateOpen:
		// Timeout expired, transition to half-open
		cb.transitionTo(StateHalfOpen)
		cb.successes = 1

	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transitionTo(StateClosed)
		}
	}
}

// transitionTo changes the circuit breaker state
func (cb *CircuitBreaker) transitionTo(newState State) {
	oldState := cb.state
	cb.state = newState
	cb.lastStateChange = time.Now()

	// Reset counters
	if newState == StateClosed {
		cb.failures = 0
		cb.successes = 0
		cb.consecutiveFails = make([]time.Time, 0)
	} else if newState == StateOpen {
		circuitBreakerTrips.WithLabelValues(cb.config.Name).Inc()
	}

	// Update metrics
	circuitBreakerState.WithLabelValues(cb.config.Name).Set(float64(newState))

	// Log transition (optional: integrate with zerolog)
	fmt.Printf("Circuit breaker %s: %s → %s\n", cb.config.Name, oldState, newState)
}

// cleanOldFailures removes failures outside the rolling window
func (cb *CircuitBreaker) cleanOldFailures() {
	cutoff := time.Now().Add(-cb.config.WindowSize)
	newFails := make([]time.Time, 0)

	for _, t := range cb.consecutiveFails {
		if t.After(cutoff) {
			newFails = append(newFails, t)
		}
	}

	cb.consecutiveFails = newFails
	cb.failures = len(newFails)
}

// GetState returns the current circuit breaker state (thread-safe)
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(StateClosed)
}
```

---

### 2. Integration in Stream Detection

**Datei:** `internal/openwebif/stream_detection.go` (erweitern)

```go
import "github.com/ManuGH/xg2g/internal/circuitbreaker"

type StreamDetector struct {
	// ... existing fields
	circuitBreakers map[int]*circuitbreaker.CircuitBreaker // key: port
}

func NewStreamDetector(receiverHost string, logger zerolog.Logger) *StreamDetector {
	sd := &StreamDetector{
		// ... existing init
		circuitBreakers: map[int]*circuitbreaker.CircuitBreaker{
			8001:  circuitbreaker.New(circuitbreaker.DefaultConfig("port_8001")),
			17999: circuitbreaker.New(circuitbreaker.DefaultConfig("port_17999")),
		},
	}
	return sd
}

// testEndpoint with circuit breaker protection
func (sd *StreamDetector) testEndpoint(ctx context.Context, candidate streamCandidate) bool {
	cb := sd.circuitBreakers[candidate.Port]
	if cb == nil {
		// No circuit breaker for this port, proceed normally
		return sd.tryRequest(ctx, http.MethodHead, candidate, false)
	}

	var success bool
	err := cb.Call(ctx, func() error {
		if sd.tryRequest(ctx, http.MethodHead, candidate, false) {
			success = true
			return nil
		}
		return fmt.Errorf("endpoint test failed")
	})

	if err != nil {
		sd.logger.Debug().
			Int("port", candidate.Port).
			Str("state", cb.GetState().String()).
			Msg("circuit breaker prevented request")
		return false
	}

	return success
}
```

---

### 3. Fallback Strategy

**Erweitern:** `DetectStreamURL()` mit automatischem Port-Switch

```go
func (sd *StreamDetector) DetectStreamURL(ctx context.Context, serviceRef, channelName string) (*StreamInfo, error) {
	// ... existing cache check

	// Build candidates with circuit breaker awareness
	candidates := sd.buildCandidates(serviceRef)

	for _, candidate := range candidates {
		cb := sd.circuitBreakers[candidate.Port]

		// Skip ports with open circuit breaker
		if cb != nil && cb.GetState() == circuitbreaker.StateOpen {
			sd.logger.Debug().
				Int("port", candidate.Port).
				Msg("skipping port with open circuit breaker")
			continue
		}

		if sd.testEndpoint(ctx, candidate) {
			// Success - return stream info
			info := &StreamInfo{
				URL:      candidate.URL,
				Port:     candidate.Port,
				TestedAt: time.Now(),
			}
			sd.cache[serviceRef] = info
			return info, nil
		}
	}

	// Fallback to default port even if circuit breaker is open
	// (better to try and fail than return nothing)
	fallback := &StreamInfo{
		URL:       "http://" + net.JoinHostPort(sd.receiverHost, "8001") + "/" + serviceRef,
		Port:      8001,
		TestedAt:  time.Now(),
		TestError: fmt.Errorf("all circuit breakers open or tests failed"),
	}

	return fallback, nil
}
```

---

## 4. Monitoring & Alerts

**Prometheus Alerts:** `deploy/monitoring/prometheus/alert-rules-circuitbreaker.yml` (neu)

```yaml
groups:
  - name: circuit_breaker
    interval: 30s
    rules:
      - alert: CircuitBreakerOpen
        expr: xg2g_circuit_breaker_state{state="1"} > 0
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Circuit breaker {{ $labels.name }} is open"
          description: "Port {{ $labels.name }} has failed repeatedly and is now blocked"

      - alert: CircuitBreakerFrequentTrips
        expr: rate(xg2g_circuit_breaker_trips_total[5m]) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Circuit breaker {{ $labels.name }} tripping frequently"
          description: "Circuit breaker tripped {{ $value }} times per second (5m average)"
```

---

## 5. Tests

**Datei:** `internal/circuitbreaker/breaker_test.go` (neu)

```go
package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreakerClosedToOpen(t *testing.T) {
	config := Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
		WindowSize:       5 * time.Second,
	}
	cb := New(config)

	// First 2 failures - should stay closed
	for i := 0; i < 2; i++ {
		err := cb.Call(context.Background(), func() error {
			return errors.New("fail")
		})
		assert.Error(t, err)
		assert.Equal(t, StateClosed, cb.GetState())
	}

	// 3rd failure - should trip to open
	err := cb.Call(context.Background(), func() error {
		return errors.New("fail")
	})
	assert.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreakerOpenToHalfOpen(t *testing.T) {
	config := Config{
		Name:             "test",
		FailureThreshold: 1,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		WindowSize:       5 * time.Second,
	}
	cb := New(config)

	// Trip to open
	cb.Call(context.Background(), func() error {
		return errors.New("fail")
	})
	assert.Equal(t, StateOpen, cb.GetState())

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Next successful call should transition to half-open
	err := cb.Call(context.Background(), func() error {
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, StateHalfOpen, cb.GetState())
}

func TestCircuitBreakerHalfOpenToClosed(t *testing.T) {
	config := Config{
		Name:             "test",
		FailureThreshold: 1,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		WindowSize:       5 * time.Second,
	}
	cb := New(config)

	// Trip to open
	cb.Call(context.Background(), func() error {
		return errors.New("fail")
	})

	// Wait and transition to half-open
	time.Sleep(100 * time.Millisecond)
	cb.Call(context.Background(), func() error {
		return nil
	})
	assert.Equal(t, StateHalfOpen, cb.GetState())

	// 2 more successes should close it
	cb.Call(context.Background(), func() error {
		return nil
	})
	assert.Equal(t, StateClosed, cb.GetState())
}
```

---

## Environment Variables

```bash
# Circuit breaker configuration
XG2G_CIRCUIT_BREAKER_ENABLED=true
XG2G_CIRCUIT_BREAKER_FAILURE_THRESHOLD=5
XG2G_CIRCUIT_BREAKER_SUCCESS_THRESHOLD=3
XG2G_CIRCUIT_BREAKER_TIMEOUT=30s
XG2G_CIRCUIT_BREAKER_WINDOW=10s
```

---

## Expected Metrics

```
# HELP xg2g_circuit_breaker_state Circuit breaker state
# TYPE xg2g_circuit_breaker_state gauge
xg2g_circuit_breaker_state{name="port_8001"} 0
xg2g_circuit_breaker_state{name="port_17999"} 1

# HELP xg2g_circuit_breaker_trips_total Total circuit breaker trips
# TYPE xg2g_circuit_breaker_trips_total counter
xg2g_circuit_breaker_trips_total{name="port_8001"} 0
xg2g_circuit_breaker_trips_total{name="port_17999"} 3
```

---

## Success Criteria

- ✅ Nach 5 Fehlern in 10s → Circuit Open
- ✅ Nach 30s Timeout → Half-Open
- ✅ Nach 3 Erfolgen → Circuit Closed
- ✅ Metrics zeigen State Transitions
- ✅ Alerts bei frequent trips

---

## Rollout

1. ✅ Circuit Breaker Package implementieren
2. ✅ Integration in Stream Detection
3. ✅ Tests (State Transitions)
4. ✅ Prometheus Alerts
5. ✅ Documentation

**Effort:** 4h
