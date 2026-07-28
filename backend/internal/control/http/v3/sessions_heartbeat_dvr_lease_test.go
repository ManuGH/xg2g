package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/require"
)

func TestSessionHeartbeat_DVRWindowSurvivesBackgroundSuspension(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	s.cfg.Sessions.LeaseTTL = 120 * time.Second

	sessionID := "550e8400-e29b-41d4-a716-446655440107"
	now := time.Now().UTC()
	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		Profile:            model.ProfileSpec{DVRWindowSec: 7200},
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: now.Add(30 * time.Second).Unix(),
		LastHeartbeatUnix:  now.Add(-31 * time.Second).Unix(),
	}))

	req := httptest.NewRequest(http.MethodPost, V3BaseURL+"/sessions/"+sessionID+"/heartbeat", nil)
	rr := httptest.NewRecorder()
	s.handleSessionHeartbeat(rr, req, sessionID)

	require.Equal(t, http.StatusOK, rr.Code)
	updated, err := st.GetSession(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.GreaterOrEqual(t, updated.LeaseExpiresAtUnix, now.Add(2*time.Hour+119*time.Second).Unix())
	require.LessOrEqual(t, updated.LeaseExpiresAtUnix, time.Now().Add(2*time.Hour+121*time.Second).Unix())
}
