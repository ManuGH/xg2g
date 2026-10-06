package sourceref

import (
	"sync/atomic"
)

// Registry provides thread-safe, atomic snapshot storage and lookup for IPTV sources.
// It uses atomic pointer swaps to guarantee zero torn reads and lock-free concurrent lookups.
type Registry struct {
	sources atomic.Pointer[map[ID]Source]
}

// NewRegistry creates an initialized, empty Registry.
func NewRegistry() *Registry {
	r := &Registry{}
	empty := make(map[ID]Source)
	r.sources.Store(&empty)
	return r
}

// Replace atomically updates the entire set of sources.
// Readers concurrently calling Lookup will see either the complete previous
// snapshot or the complete new snapshot, with zero lock contention or torn reads.
// Passing a nil or empty slice clears the registry.
// If two sources produce an identical ID but possess distinct canonical URLs (a true HMAC collision),
// Replace aborts and returns ErrCollision without modifying the registry.
// Sources with identical IDs and matching canonical URLs are treated as equivalent (e.g. harmless
// formatting differences like scheme/host casing, default port 80/443, trailing fragment, or
// duplicate channel entries across bouquets). For equivalent sources, the first occurrence is kept
// and subsequent occurrences are skipped without error. Zero/empty sources are skipped.
func (r *Registry) Replace(snapshot []Source) error {
	if snapshot == nil {
		empty := make(map[ID]Source)
		r.sources.Store(&empty)
		return nil
	}

	m := make(map[ID]Source, len(snapshot))
	for _, s := range snapshot {
		if s.IsZero() || s.ID() == "" {
			continue
		}
		if existing, found := m[s.ID()]; found {
			if existing.canonicalURL() != s.canonicalURL() {
				return ErrCollision
			}
			// Equivalent source (same ID and same canonical URL); keep first
			continue
		}
		m[s.ID()] = s
	}
	r.sources.Store(&m)
	return nil
}

// Register adds a single IPTV source to the registry using atomic CompareAndSwap copy-on-write semantics.
// It is thread-safe and lock-free.
// If s is zero or has an empty ID, Register returns nil without modifying the registry.
// If an entry with s.ID() already exists:
//   - If the existing entry has matching canonicalURL, Register treats it as equivalent and returns nil idempotently.
//   - If the existing entry has differing canonicalURL, Register returns ErrCollision.
//
// If s is new, it allocates a new map, inserts s, and attempts an atomic CAS. If another goroutine
// modified the registry concurrently, it retries until success.
func (r *Registry) Register(s Source) error {
	if s.IsZero() || s.ID() == "" {
		return nil
	}

	for {
		curr := r.sources.Load()
		if curr != nil {
			if existing, found := (*curr)[s.ID()]; found {
				if existing.canonicalURL() != s.canonicalURL() {
					return ErrCollision
				}
				// Idempotent: already registered with matching canonical URL
				return nil
			}
		}

		currLen := 0
		if curr != nil {
			currLen = len(*curr)
		}
		newMap := make(map[ID]Source, currLen+1)
		if curr != nil {
			for k, v := range *curr {
				newMap[k] = v
			}
		}
		newMap[s.ID()] = s

		if r.sources.CompareAndSwap(curr, &newMap) {
			return nil
		}
	}
}

// Lookup retrieves a Source by its opaque ID.
// If the ID is not found, ErrNotFound is returned without echoing the input.
func (r *Registry) Lookup(id ID) (Source, error) {
	m := r.sources.Load()
	if m == nil {
		return Source{}, ErrNotFound
	}
	s, ok := (*m)[id]
	if !ok {
		return Source{}, ErrNotFound
	}
	return s, nil
}

// Len returns the current number of registered sources.
func (r *Registry) Len() int {
	m := r.sources.Load()
	if m == nil {
		return 0
	}
	return len(*m)
}

// Snapshot returns a copy of all currently registered sources.
func (r *Registry) Snapshot() []Source {
	m := r.sources.Load()
	if m == nil || len(*m) == 0 {
		return nil
	}
	res := make([]Source, 0, len(*m))
	for _, s := range *m {
		res = append(res, s)
	}
	return res
}
