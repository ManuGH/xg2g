package model

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionInactivityTTL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		base         time.Duration
		dvrWindowSec int
		want         time.Duration
	}{
		{
			name: "live only keeps short lease",
			base: 2 * time.Minute,
			want: 2 * time.Minute,
		},
		{
			name:         "two hour DVR keeps window plus resume grace",
			base:         2 * time.Minute,
			dvrWindowSec: 7200,
			want:         2*time.Hour + 2*time.Minute,
		},
		{
			name:         "operator DVR window is preserved",
			base:         2 * time.Minute,
			dvrWindowSec: 16200,
			want:         4*time.Hour + 32*time.Minute,
		},
		{
			name:         "negative base cannot shorten DVR window",
			base:         -time.Minute,
			dvrWindowSec: 60,
			want:         time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, SessionInactivityTTL(tt.base, tt.dvrWindowSec))
		})
	}
}

func TestSessionInactivityTTL_SaturatesOverflow(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("a 32-bit int cannot represent a duration large enough to overflow")
	}

	overflowWindowSec := int(int64(math.MaxInt64/int64(time.Second)) + 1)
	require.Equal(t,
		time.Duration(math.MaxInt64),
		SessionInactivityTTL(time.Second, overflowWindowSec),
	)
}
