package profiles

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
)

func TestResolveWithConfig_DoesNotReadEnvironmentMidPlanning(t *testing.T) {
	t.Setenv("XG2G_SAFARI_CPU_PRESET", "fast")
	snapshot := LoadConfigSnapshot()
	t.Setenv("XG2G_SAFARI_CPU_PRESET", "slow")

	capability := &scan.Capability{Interlaced: true}
	fromSnapshot := ResolveWithConfig(ProfileSafari, "Safari/17", 0, capability, GPUBackendNone, HWAccelAuto, snapshot)
	fromNewEnvironment := ResolveWithConfig(ProfileSafari, "Safari/17", 0, capability, GPUBackendNone, HWAccelAuto, LoadConfigSnapshot())

	assert.Equal(t, "fast", fromSnapshot.Preset)
	assert.Equal(t, "slow", fromNewEnvironment.Preset)
}

func TestResolverOwnsServiceRefPolicySnapshot(t *testing.T) {
	cfg := DefaultConfigSnapshot()
	cfg.SafariForceCopyServiceRefs = []string{"service:one"}
	resolver := NewResolver(cfg)
	cfg.SafariForceCopyServiceRefs[0] = "mutated"

	first := resolver.ConfigSnapshot()
	assert.Equal(t, []string{"service:one"}, first.SafariForceCopyServiceRefs)
	first.SafariForceCopyServiceRefs[0] = "mutated-again"
	assert.Equal(t, []string{"service:one"}, resolver.ConfigSnapshot().SafariForceCopyServiceRefs)
}

func TestResolverInitializationIsExplicit(t *testing.T) {
	assert.False(t, (Resolver{}).IsInitialized())
	assert.True(t, NewResolver(DefaultConfigSnapshot()).IsInitialized())
}
