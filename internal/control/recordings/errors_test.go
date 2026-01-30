package recordings

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/stretchr/testify/assert"
)

func TestClassify_PlaybackErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorClass
	}{
		{
			name: "Playback ErrUpstream -> ClassUpstream",
			err:  playback.ErrUpstream,
			want: ClassUpstream,
		},
		{
			name: "Playback ErrNotFound -> ClassNotFound",
			err:  playback.ErrNotFound,
			want: ClassNotFound,
		},
		{
			name: "Playback ErrPreparing -> ClassPreparing",
			err:  playback.ErrPreparing,
			want: ClassPreparing,
		},
		{
			name: "Playback ErrForbidden -> ClassForbidden",
			err:  playback.ErrForbidden,
			want: ClassForbidden,
		},
		{
			name: "Local ErrValidation (via InvalidArgument)",
			err:  ErrInvalidArgument{Field: "foo", Reason: "bar"},
			want: ClassInvalidArgument,
		},
		{
			name: "Unknown error -> ClassInternal",
			err:  assert.AnError,
			want: ClassInternal,
		},
		{
			name: "Nil error -> empty",
			err:  nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
