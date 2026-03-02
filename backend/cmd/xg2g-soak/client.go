// Package main - API client for xg2g session management.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SessionClient provides xg2g API capabilities for the harness.
type SessionClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewSessionClient creates a new xg2g API client.
func NewSessionClient(baseURL, token string) *SessionClient {
	return &SessionClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// IntentRequest represents a session intent.
type IntentRequest struct {
	ServiceRef string            `json:"serviceRef"`
	Intent     string            `json:"intent"`
	Params     map[string]string `json:"params,omitempty"`
}

// IntentResponse represents the API response.
type IntentResponse struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// SessionResult captures the result of a session start attempt.
type SessionResult struct {
	SessionID       string
	HTTPStatus      int
	AdmissionReason string // From X-Admission-Factor header
	Error           error
}

// StartSession attempts to start a session with the given priority profile.
// Returns SessionResult with sessionID, httpStatus, admission reason, and error.
func (c *SessionClient) StartSession(serviceRef string, priority string) SessionResult {
	req := IntentRequest{
		ServiceRef: serviceRef,
		Intent:     "stream_start",
		Params: map[string]string{
			"priority": priority,
		},
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/v3/sessions/intents", bytes.NewReader(body))
	if err != nil {
		return SessionResult{Error: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return SessionResult{Error: err}
	}
	defer func() {
		// best-effort close
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v\n", err)
		}
	}()

	result := SessionResult{
		HTTPStatus:      resp.StatusCode,
		AdmissionReason: resp.Header.Get("X-Admission-Factor"),
	}

	respBody, _ := io.ReadAll(resp.Body)

	var intentResp IntentResponse
	if err := json.Unmarshal(respBody, &intentResp); err != nil {
		result.Error = fmt.Errorf("failed to unmarshal response: %w", err)
		return result
	}
	result.SessionID = intentResp.SessionID

	return result
}

// StopSession stops a session by ID.
func (c *SessionClient) StopSession(sessionID string) error {
	req := IntentRequest{
		Intent: "stream_stop",
		Params: map[string]string{
			"sessionId": sessionID,
		},
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/v3/sessions/intents", bytes.NewReader(body))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() {
		// best-effort close
		if err := resp.Body.Close(); err != nil {
			// pure soak client, fmt.Println is acceptable
			fmt.Printf("failed to close stop response body: %v\n", err)
		}
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("stop session failed: %d", resp.StatusCode)
	}

	return nil
}

// StopAllSessions stops all sessions, returning count of errors.
func (c *SessionClient) StopAllSessions(sessionIDs []string) int {
	errorCount := 0
	for _, sid := range sessionIDs {
		if err := c.StopSession(sid); err != nil {
			errorCount++
		}
	}
	return errorCount
}

// GetSessionStatus returns the HTTP status code for a session info request.
func (c *SessionClient) GetSessionStatus(sessionID string) (int, error) {
	httpReq, err := http.NewRequest("GET", c.baseURL+"/api/v3/sessions/"+sessionID, nil)
	if err != nil {
		return 0, err
	}
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close status response body: %v\n", err)
		}
	}()
	return resp.StatusCode, nil
}
