// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package deadline

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// RouteDeadlineClass categorizes a route by its response lifetime.
type RouteDeadlineClass uint8

const (
	RouteDeadlineUnknown RouteDeadlineClass = iota
	RouteDeadlineAPIBounded
	RouteDeadlineMediaBounded
	RouteDeadlineStreaming
)

func (c RouteDeadlineClass) String() string {
	switch c {
	case RouteDeadlineUnknown:
		return "Unknown"
	case RouteDeadlineAPIBounded:
		return "APIBounded"
	case RouteDeadlineMediaBounded:
		return "MediaBounded"
	case RouteDeadlineStreaming:
		return "Streaming"
	default:
		return fmt.Sprintf("UnknownRouteDeadlineClass(%d)", c)
	}
}

// CapabilityState represents the capability verification lifecycle.
type CapabilityState uint8

const (
	CapabilityUnknown CapabilityState = iota
	CapabilityUnsupported
	CapabilityDeclared
	CapabilityVerified
)

func (s CapabilityState) String() string {
	switch s {
	case CapabilityUnknown:
		return "Unknown"
	case CapabilityUnsupported:
		return "Unsupported"
	case CapabilityDeclared:
		return "Declared"
	case CapabilityVerified:
		return "Verified"
	default:
		return fmt.Sprintf("UnknownCapabilityState(%d)", s)
	}
}

// ResponseWriterEquivalenceClass identifies a concrete writer wrapper stack.
type ResponseWriterEquivalenceClass string

const (
	EquivalenceClassOuterStandard   ResponseWriterEquivalenceClass = "outer-standard"
	EquivalenceClassOuterCompressed ResponseWriterEquivalenceClass = "outer-compressed"
	EquivalenceClassV3Standard      ResponseWriterEquivalenceClass = "v3-standard"
	EquivalenceClassV3Compressed    ResponseWriterEquivalenceClass = "v3-compressed"
)

// VerifiedStackEvidence records empirical capabilities for one writer stack.
type VerifiedStackEvidence struct {
	EquivalenceClass          ResponseWriterEquivalenceClass
	SetWriteDeadlineVerified  bool
	FlushVerified             bool
	HijackVerified            bool
	UpgradeTransitionVerified bool
}

// RoutePolicy is the neutral static deadline policy bound to one route.
type RoutePolicy struct {
	Class                RouteDeadlineClass
	RequiresFlush        bool
	MayUpgradePerRequest bool
}

// DeadlineTimeouts contains route-class-specific write timeouts.
type DeadlineTimeouts struct {
	APIWriteTimeout      time.Duration
	MediaWriteTimeout    time.Duration
	StreamingIdleTimeout time.Duration
}

func DefaultTimeouts() DeadlineTimeouts {
	return DeadlineTimeouts{
		APIWriteTimeout:      5 * time.Second,
		MediaWriteTimeout:    30 * time.Second,
		StreamingIdleTimeout: 15 * time.Second,
	}
}

func (t DeadlineTimeouts) Validate() error {
	if t.APIWriteTimeout <= 0 {
		return fmt.Errorf("APIWriteTimeout must be > 0 (got %v)", t.APIWriteTimeout)
	}
	if t.MediaWriteTimeout < t.APIWriteTimeout {
		return fmt.Errorf("MediaWriteTimeout (%v) must be >= APIWriteTimeout (%v)", t.MediaWriteTimeout, t.APIWriteTimeout)
	}
	if t.StreamingIdleTimeout <= 0 {
		return fmt.Errorf("StreamingIdleTimeout must be > 0 (got %v)", t.StreamingIdleTimeout)
	}
	return nil
}

// RegistrationKey uniquely identifies one registration instance.
type RegistrationKey struct {
	RouterID string
	Method   string
	Pattern  string
	Ordinal  int
}

func (k RegistrationKey) String() string {
	return fmt.Sprintf("[%s#%d] %s %s", k.RouterID, k.Ordinal, k.Method, k.Pattern)
}

var supportedMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodDelete:  {},
	http.MethodOptions: {},
	http.MethodPatch:   {},
	http.MethodTrace:   {},
	http.MethodConnect: {},
}

// NormalizeRegistrationKey validates and normalizes a registration identity.
func NormalizeRegistrationKey(routerID, method, rawPattern, mountPrefix string, ordinal int) (RegistrationKey, error) {
	routerID = strings.TrimSpace(routerID)
	if routerID != "outer" && routerID != "v3" {
		return RegistrationKey{}, fmt.Errorf("invalid router ID %q", routerID)
	}

	method = strings.ToUpper(strings.TrimSpace(method))
	if _, ok := supportedMethods[method]; !ok {
		return RegistrationKey{}, fmt.Errorf("invalid HTTP method %q", method)
	}
	if ordinal < 0 {
		return RegistrationKey{}, fmt.Errorf("ordinal must be non-negative")
	}

	rawPattern = strings.TrimSpace(rawPattern)
	if rawPattern == "" || rawPattern[0] != '/' || strings.ContainsAny(rawPattern, "\r\n\t ") {
		return RegistrationKey{}, fmt.Errorf("invalid route pattern %q", rawPattern)
	}

	mountPrefix = strings.TrimSpace(mountPrefix)
	if mountPrefix != "" && mountPrefix != "/" && mountPrefix[0] != '/' {
		return RegistrationKey{}, fmt.Errorf("invalid mount prefix %q", mountPrefix)
	}

	pattern := rawPattern
	if mountPrefix != "" && mountPrefix != "/" {
		prefix := "/" + strings.Trim(mountPrefix, "/")
		if rawPattern == "/" {
			pattern = prefix
		} else {
			pattern = prefix + "/" + strings.TrimLeft(rawPattern, "/")
		}
	}
	for strings.Contains(pattern, "//") {
		pattern = strings.ReplaceAll(pattern, "//", "/")
	}

	return RegistrationKey{
		RouterID: routerID,
		Method:   method,
		Pattern:  pattern,
		Ordinal:  ordinal,
	}, nil
}

// PolicyBindingSnapshot is an immutable copy of committed bindings.
type PolicyBindingSnapshot struct {
	entries map[RegistrationKey]RoutePolicy
}

func (s PolicyBindingSnapshot) Len() int {
	return len(s.entries)
}

func (s PolicyBindingSnapshot) Lookup(key RegistrationKey) (RoutePolicy, bool) {
	policy, ok := s.entries[key]
	return policy, ok
}

func (s PolicyBindingSnapshot) Keys() []RegistrationKey {
	keys := make([]RegistrationKey, 0, len(s.entries))
	for key := range s.entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].RouterID != keys[j].RouterID {
			return keys[i].RouterID < keys[j].RouterID
		}
		if keys[i].Method != keys[j].Method {
			return keys[i].Method < keys[j].Method
		}
		if keys[i].Pattern != keys[j].Pattern {
			return keys[i].Pattern < keys[j].Pattern
		}
		return keys[i].Ordinal < keys[j].Ordinal
	})
	return keys
}

// PolicyBindingRegistry is local to one router build.
type PolicyBindingRegistry struct {
	operationMu  sync.Mutex
	mu           sync.RWMutex
	entries      map[RegistrationKey]RoutePolicy
	nextOrdinals map[string]int
	tuplePolicy  map[string]RoutePolicy
}

func NewPolicyBindingRegistry() *PolicyBindingRegistry {
	return &PolicyBindingRegistry{
		entries:      make(map[RegistrationKey]RoutePolicy),
		nextOrdinals: make(map[string]int),
		tuplePolicy:  make(map[string]RoutePolicy),
	}
}

// BindingReservation serializes one prepare/register/commit operation. A caller
// must call Commit or Cancel exactly once.
type BindingReservation struct {
	registry *PolicyBindingRegistry
	key      RegistrationKey
	policy   RoutePolicy
	done     bool
}

func tupleKey(key RegistrationKey) string {
	return key.RouterID + "\x00" + key.Method + "\x00" + key.Pattern
}

// ReserveBinding validates a prospective binding without publishing it.
func (r *PolicyBindingRegistry) ReserveBinding(
	routerID, method, rawPattern, mountPrefix string,
	policy RoutePolicy,
) (*BindingReservation, error) {
	if r == nil {
		return nil, fmt.Errorf("nil PolicyBindingRegistry")
	}
	if policy.Class == RouteDeadlineUnknown || policy.Class > RouteDeadlineStreaming {
		return nil, fmt.Errorf("invalid route policy class %s", policy.Class)
	}

	r.operationMu.Lock()
	r.mu.RLock()
	base, err := NormalizeRegistrationKey(routerID, method, rawPattern, mountPrefix, 0)
	if err != nil {
		r.mu.RUnlock()
		r.operationMu.Unlock()
		return nil, err
	}
	tuple := tupleKey(base)
	if existing, ok := r.tuplePolicy[tuple]; ok && existing != policy {
		r.mu.RUnlock()
		r.operationMu.Unlock()
		return nil, fmt.Errorf("conflicting policy for %s %s on router %s", base.Method, base.Pattern, base.RouterID)
	}
	base.Ordinal = r.nextOrdinals[tuple]
	if _, exists := r.entries[base]; exists {
		r.mu.RUnlock()
		r.operationMu.Unlock()
		return nil, fmt.Errorf("duplicate policy binding key %s", base)
	}
	r.mu.RUnlock()

	return &BindingReservation{registry: r, key: base, policy: policy}, nil
}

func (r *BindingReservation) Key() RegistrationKey {
	if r == nil {
		return RegistrationKey{}
	}
	return r.key
}

// Commit publishes a reservation. It cannot fail after a successful reserve.
func (r *BindingReservation) Commit() {
	if r == nil || r.done {
		return
	}
	registry := r.registry
	registry.mu.Lock()
	tuple := tupleKey(r.key)
	registry.entries[r.key] = r.policy
	registry.tuplePolicy[tuple] = r.policy
	registry.nextOrdinals[tuple] = r.key.Ordinal + 1
	registry.mu.Unlock()
	r.done = true
	registry.operationMu.Unlock()
}

// Cancel releases a reservation without publishing it.
func (r *BindingReservation) Cancel() {
	if r == nil || r.done {
		return
	}
	r.done = true
	r.registry.operationMu.Unlock()
}

func (r *PolicyBindingRegistry) RecordBinding(
	routerID, method, rawPattern, mountPrefix string,
	policy RoutePolicy,
) (RegistrationKey, error) {
	reservation, err := r.ReserveBinding(routerID, method, rawPattern, mountPrefix, policy)
	if err != nil {
		return RegistrationKey{}, err
	}
	key := reservation.Key()
	reservation.Commit()
	return key, nil
}

func (r *PolicyBindingRegistry) Snapshot() PolicyBindingSnapshot {
	if r == nil {
		return PolicyBindingSnapshot{entries: map[RegistrationKey]RoutePolicy{}}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make(map[RegistrationKey]RoutePolicy, len(r.entries))
	for key, policy := range r.entries {
		entries[key] = policy
	}
	return PolicyBindingSnapshot{entries: entries}
}
