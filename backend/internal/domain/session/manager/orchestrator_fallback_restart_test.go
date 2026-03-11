package manager

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/require"
)

func TestTransitionStarting_AllowsTerminalFallbackRestart(t *testing.T) {
	st := store.NewMemoryStore()
	const sid = "sess-fallback-restart"

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:        sid,
		State:            model.SessionStopped,
		FallbackReason:   "client_report:code=3",
		FallbackAtUnix:   123,
		CreatedAtUnix:    111,
		UpdatedAtUnix:    112,
		StopReason:       "CLIENT_STOP",
		Reason:           model.RClientStop,
		ReasonDetailCode: model.DContextCanceled,
		ContextData: map[string]string{
			model.CtxKeyTunerSlot: "7",
		},
	}))

	orch := &Orchestrator{Store: st}
	err := orch.transitionStarting(context.Background(), model.StartSessionEvent{SessionID: sid}, &sessionContext{Mode: model.ModeLive}, 2)
	require.NoError(t, err)

	rec, err := st.GetSession(context.Background(), sid)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, model.SessionStarting, rec.State)
	require.Equal(t, int64(111), rec.CreatedAtUnix)
	require.Empty(t, rec.StopReason)
	require.Empty(t, rec.Reason)
	require.Empty(t, rec.ReasonDetailCode)
	require.Equal(t, "2", rec.ContextData[model.CtxKeyTunerSlot])
}

func TestTransitionStarting_RejectsTerminalSessionWithoutFallbackRestart(t *testing.T) {
	st := store.NewMemoryStore()
	const sid = "sess-terminal-stop"

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:     sid,
		State:         model.SessionStopped,
		CreatedAtUnix: 111,
	}))

	orch := &Orchestrator{Store: st}
	err := orch.transitionStarting(context.Background(), model.StartSessionEvent{SessionID: sid}, &sessionContext{Mode: model.ModeLive}, 1)
	require.ErrorContains(t, err, "session state STOPPED, aborting start")
}
