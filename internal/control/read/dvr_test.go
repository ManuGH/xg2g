package read

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDvrSource struct {
	cap openwebif.TimerChangeCap
	err error
}

func (m *mockDvrSource) GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error) {
	return nil, nil
}

func (m *mockDvrSource) DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error) {
	return m.cap, m.err
}

func TestGetDvrCapabilities_CanEditReflectsCapability(t *testing.T) {
	src := &mockDvrSource{
		cap: openwebif.TimerChangeCap{Supported: false},
	}

	caps, err := GetDvrCapabilities(context.Background(), src)
	require.NoError(t, err)
	assert.False(t, caps.CanEdit, "CanEdit should respect DetectTimerChange capability")
	assert.False(t, caps.ReceiverAware, "ReceiverAware should track edit capability")
}

func TestGetDvrCapabilities_CanEditFailClosedOnError(t *testing.T) {
	src := &mockDvrSource{
		cap: openwebif.TimerChangeCap{Supported: true},
		err: assert.AnError,
	}

	caps, err := GetDvrCapabilities(context.Background(), src)
	require.NoError(t, err)
	assert.False(t, caps.CanEdit, "CanEdit should fail-closed on capability detection errors")
	assert.False(t, caps.ReceiverAware, "ReceiverAware should fail-closed on capability detection errors")
}
