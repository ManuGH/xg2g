package decision

import (
	"context"
	"fmt"
	"sync"

	"github.com/ManuGH/xg2g/internal/normalize"
)

// ShadowDivergence captures a typed-vs-legacy predicate mismatch while the
// legacy decision path remains authoritative.
type ShadowDivergence struct {
	Predicate                     string   `json:"predicate"`
	LegacyCanVideo                bool     `json:"legacyCanVideo"`
	LegacyVideoRepairRequired     bool     `json:"legacyVideoRepairRequired"`
	LegacyCompatibleWithoutRepair bool     `json:"legacyCompatibleWithoutRepair"`
	NewCompatible                 bool     `json:"newCompatible"`
	NewReasons                    []string `json:"newReasons,omitempty"`
}

func (d ShadowDivergence) Normalized() ShadowDivergence {
	d.Predicate = normalize.Token(d.Predicate)
	if len(d.NewReasons) == 0 {
		return d
	}

	seen := make(map[string]struct{}, len(d.NewReasons))
	out := make([]string, 0, len(d.NewReasons))
	for _, reason := range d.NewReasons {
		normalized := normalize.Token(reason)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	d.NewReasons = out
	return d
}

func (d ShadowDivergence) Valid() error {
	if d.Predicate == "" {
		return fmt.Errorf("shadow divergence requires predicate")
	}
	return nil
}

type ShadowCollector struct {
	mu          sync.Mutex
	divergences []ShadowDivergence
}

func NewShadowCollector() *ShadowCollector {
	return &ShadowCollector{}
}

func (c *ShadowCollector) Add(divergence ShadowDivergence) {
	if c == nil {
		return
	}
	divergence = divergence.Normalized()
	if err := divergence.Valid(); err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.divergences = append(c.divergences, divergence)
}

func (c *ShadowCollector) Divergences() []ShadowDivergence {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.divergences) == 0 {
		return nil
	}

	out := make([]ShadowDivergence, len(c.divergences))
	for i, divergence := range c.divergences {
		out[i] = divergence
		if len(divergence.NewReasons) == 0 {
			continue
		}
		out[i].NewReasons = append([]string(nil), divergence.NewReasons...)
	}
	return out
}

type shadowCollectorContextKey struct{}

func WithShadowCollector(ctx context.Context, collector *ShadowCollector) context.Context {
	if ctx == nil || collector == nil {
		return ctx
	}
	return context.WithValue(ctx, shadowCollectorContextKey{}, collector)
}

func ShadowDivergencesFromContext(ctx context.Context) []ShadowDivergence {
	return shadowCollectorFromContext(ctx).Divergences()
}

func shadowCollectorFromContext(ctx context.Context) *ShadowCollector {
	if ctx == nil {
		return nil
	}
	collector, _ := ctx.Value(shadowCollectorContextKey{}).(*ShadowCollector)
	return collector
}
