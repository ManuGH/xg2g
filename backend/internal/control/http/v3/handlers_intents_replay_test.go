package v3

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	v3bus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/stretchr/testify/require"
)

func TestHandleV3Intents_TerminalReplayStartsFreshSession(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())

	bus, ok := s.v3Bus.(*v3bus.MemoryBus)
	require.True(t, ok)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	sub, err := bus.Subscribe(ctx, string(model.EventStartSession))
	require.NoError(t, err)
	defer func() { _ = sub.Close() }()

	const svcRef = "1:0:1:445D:453:1:C00000:0:0:0:"

	send := func() v3api.IntentResponse {
		body := intentBodyWithValidJWT(t, svcRef, "", "live", "corr-replay-terminal-001")
		req := httptest.NewRequest(http.MethodPost, V3BaseURL+"/intents", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		NewRouter(s, RouterOptions{BaseURL: V3BaseURL}).ServeHTTP(rr, req)
		require.Equal(t, http.StatusAccepted, rr.Code)

		return decodeIntentResponse(t, rr.Body.Bytes())
	}

	first := send()
	require.Equal(t, "accepted", first.Status)

	select {
	case <-sub.C():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected first session.start event")
	}

	_, err = st.UpdateSession(context.Background(), first.SessionID, func(sess *model.SessionRecord) error {
		sess.State = model.SessionFailed
		sess.Reason = model.RUpstreamCorrupt
		return nil
	})
	require.NoError(t, err)

	second := send()
	require.Equal(t, "accepted", second.Status)
	require.NotEqual(t, first.SessionID, second.SessionID)

	select {
	case <-sub.C():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected second session.start event after terminal replay cleanup")
	}

	sessions, err := st.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, sessions, 2)
}
