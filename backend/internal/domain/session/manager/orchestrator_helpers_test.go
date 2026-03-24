package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestBuildSessionContext(t *testing.T) {
	o := &Orchestrator{}

	t.Run("default to live", func(t *testing.T) {
		session := &model.SessionRecord{}
		event := model.StartSessionEvent{ServiceRef: "ref1"}
		ctx, err := o.buildSessionContext(session, event)
		require.NoError(t, err)
		assert.Equal(t, model.ModeLive, ctx.Mode)
		assert.Equal(t, "ref1", ctx.ServiceRef)
		assert.False(t, ctx.IsVOD)
	})

	t.Run("recording mode updates", func(t *testing.T) {
		session := &model.SessionRecord{
			ContextData: map[string]string{
				model.CtxKeyMode:   string(model.ModeRecording),
				model.CtxKeySource: "record1",
			},
		}
		event := model.StartSessionEvent{ServiceRef: "ref1"}
		ctx, err := o.buildSessionContext(session, event)
		require.NoError(t, err)
		assert.Equal(t, model.ModeRecording, ctx.Mode)
		assert.Equal(t, "record1", ctx.ServiceRef)
		assert.True(t, ctx.IsVOD)
	})

	t.Run("recording without source fails", func(t *testing.T) {
		session := &model.SessionRecord{
			ContextData: map[string]string{
				model.CtxKeyMode: string(model.ModeRecording),
			},
		}
		event := model.StartSessionEvent{ServiceRef: ""} // empty event ref too
		_, err := o.buildSessionContext(session, event)
		require.Error(t, err)
	})

	t.Run("invalid mode falls back to live", func(t *testing.T) {
		session := &model.SessionRecord{
			ContextData: map[string]string{
				model.CtxKeyMode: "UNKNOWN",
			},
		}
		event := model.StartSessionEvent{ServiceRef: "ref1"}
		ctx, err := o.buildSessionContext(session, event)
		require.NoError(t, err)
		assert.Equal(t, model.ModeLive, ctx.Mode)
	})
}

func TestShouldRetryStartupWaitFailure(t *testing.T) {
	assert.False(t, shouldRetryStartupWaitFailure(model.RProcessEnded, "upstream stream ended prematurely", defaultStartupProcessRetryLimit))
	assert.False(t, shouldRetryStartupWaitFailure(model.RBadRequest, "upstream stream ended prematurely", 0))

	assert.True(t, shouldRetryStartupWaitFailure(model.RProcessEnded, "upstream stream ended prematurely!!", 0))
	assert.True(t, shouldRetryStartupWaitFailure(model.RProcessEnded, "failed to open upstream input...", 0))
	assert.True(t, shouldRetryStartupWaitFailure(model.RProcessEnded, "invalid upstream input data", 0))
	assert.False(t, shouldRetryStartupWaitFailure(model.RProcessEnded, "some other crash", 0))
}

func TestDetectTerminationCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cause := detectTerminationCause(ctx, nil)
	assert.True(t, cause.IsClean)

	cancel()
	cause = detectTerminationCause(ctx, nil)
	assert.True(t, cause.ContextCancelled)
	assert.False(t, cause.IsClean)

	cause = detectTerminationCause(context.Background(), errors.New("boom"))
	assert.Equal(t, "boom", cause.Error.Error())
}

func TestPlaylistReadyTimeout(t *testing.T) {
	o := &Orchestrator{
		PlaylistReadyTimeout:         10 * time.Second,
		SafariPlaylistReadyTimeout:   15 * time.Second,
		RecoveryPlaylistReadyTimeout: 20 * time.Second,
	}

	assert.Equal(t, o.playlistReadyTimeout(model.ProfileSpec{Name: "default"}, true), defaultVODPlaylistReadyTimeout)
	assert.Equal(t, o.playlistReadyTimeout(model.ProfileSpec{Name: "safari_dirty"}, false), 20*time.Second)
	assert.Equal(t, o.playlistReadyTimeout(model.ProfileSpec{Name: "safari"}, false), 15*time.Second)
	assert.Equal(t, o.playlistReadyTimeout(model.ProfileSpec{Name: "default"}, false), 10*time.Second)
}
