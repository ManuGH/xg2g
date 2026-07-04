package v3

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionEvents_SSEStream(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	sessionID := "550e8400-e29b-41d4-a716-446655440099"
	now := time.Now().UTC()

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:          sessionID,
		State:              model.SessionReady,
		ServiceRef:         "1:0:1:445D:453:1:C00000:0:0:0:",
		HeartbeatInterval:  30,
		LeaseExpiresAtUnix: now.Add(30 * time.Second).Unix(),
		LastHeartbeatUnix:  now.Add(-31 * time.Second).Unix(),
	}))

	ts := httptest.NewServer(NewRouter(s, RouterOptions{BaseURL: V3BaseURL}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+V3BaseURL+"/sessions/"+sessionID+"/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	scanner := bufio.NewScanner(resp.Body)

	// Read initial state event
	var eventType, eventData string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			eventData = strings.TrimPrefix(line, "data: ")
			break // Got complete event
		}
	}

	assert.Equal(t, string(model.EventSessionStateChanged), eventType)
	var initialEvent model.SessionStateChangedEvent
	require.NoError(t, json.Unmarshal([]byte(eventData), &initialEvent))
	assert.Equal(t, sessionID, initialEvent.SessionID)
	assert.Equal(t, model.SessionReady, initialEvent.State)

	// Publish new event to the bus
	newEvent := model.SessionStateChangedEvent{
		Type:        model.EventSessionStateChanged,
		SessionID:   sessionID,
		State:       model.SessionStopped,
		Reason:      "USER_REQUEST",
		UpdatedAtUN: time.Now().Unix(),
	}
	require.NoError(t, s.v3Bus.Publish(context.Background(), string(model.EventSessionStateChanged), newEvent))

	// Read streamed event
	eventType = ""
	eventData = ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			eventData = strings.TrimPrefix(line, "data: ")
			break // Got complete event
		}
	}

	assert.Equal(t, string(model.EventSessionStateChanged), eventType)
	var streamedEvent model.SessionStateChangedEvent
	require.NoError(t, json.Unmarshal([]byte(eventData), &streamedEvent))
	assert.Equal(t, sessionID, streamedEvent.SessionID)
	assert.Equal(t, model.SessionStopped, streamedEvent.State)
	assert.Equal(t, model.ReasonCode("USER_REQUEST"), streamedEvent.Reason)
}
