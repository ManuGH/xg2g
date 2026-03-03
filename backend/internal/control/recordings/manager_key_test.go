package recordings

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProbeManager_ResolveKey_Hygiene(t *testing.T) {
	pm := &probeManager{}

	t.Run("Stable hashing for different encodings of same file path", func(t *testing.T) {
		// Case A: Relative vs Absolute vs Slash-noise
		// filepath.Clean handles these.
		p1 := "/tmp/recording one.ts"
		p2 := "/tmp/../tmp/recording one.ts"

		// Simulate resolveSource: Cleanup + URL creation
		// Note: we use "os" package here if we wanted real absolute, but we'll stick to string cleaning
		cp1 := filepath.Clean(p1)
		cp2 := filepath.Clean(p2)
		u1 := "file://" + cp1
		u2 := "file://" + cp2

		key1 := pm.ResolveKey("ref1", u1)
		key2 := pm.ResolveKey("ref1", u2)

		assert.Equal(t, u1, u2, "Canonical source strings must match")
		assert.Equal(t, key1, key2, "Singleflight key must be identical for same file regardless of path noise")
		assert.NotEmpty(t, key1)
	})

	t.Run("Different files get different keys", func(t *testing.T) {
		s1 := "file:///tmp/a.ts"
		s2 := "file:///tmp/b.ts"

		key1 := pm.ResolveKey("refA", s1)
		key2 := pm.ResolveKey("refB", s2)

		assert.NotEqual(t, key1, key2)
	})

	t.Run("Empty source falls back to serviceRef", func(t *testing.T) {
		key := pm.ResolveKey("service:ref", "")
		assert.Equal(t, "service:ref", key)
	})
}
