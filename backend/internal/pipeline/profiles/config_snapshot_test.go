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
