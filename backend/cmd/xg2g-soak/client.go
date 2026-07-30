package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxErrorBodyBytes = 8 << 10

type playbackCapabilities struct {
	CapabilitiesVersion int      `json:"capabilitiesVersion"`
	Container           []string `json:"container"`
	VideoCodecs         []string `json:"videoCodecs"`
	AudioCodecs         []string `json:"audioCodecs"`
}

func defaultPlaybackCapabilities() playbackCapabilities {
	return playbackCapabilities{
		CapabilitiesVersion: 3,
		Container:           []string{"hls"},
		VideoCodecs:         []string{"h264"},
		AudioCodecs:         []string{"aac"},
	}
}

type playbackInfoRequest struct {
	ServiceRef   string               `json:"serviceRef"`
	Capabilities playbackCapabilities `json:"capabilities"`
}

type playbackInfoResponse struct {
	PlaybackDecisionToken string `json:"playbackDecisionToken"`
}

type intentRequest struct {
	Type                  string                `json:"type"`
	ServiceRef            string                `json:"serviceRef,omitempty"`
	PlaybackDecisionToken string                `json:"playbackDecisionToken,omitempty"`
	SessionID             string                `json:"sessionId,omitempty"`
	IdempotencyKey        string                `json:"idempotencyKey,omitempty"`
	Client                *playbackCapabilities `json:"client,omitempty"`
}

type intentAcceptedResponse struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
}

type sessionResponse struct {
	SessionID                string `json:"sessionId"`
	RequestID                string `json:"requestId"`
	State                    string `json:"state"`
	Reason                   string `json:"reason"`
	ReasonDetail             string `json:"reasonDetail"`
	HeartbeatIntervalSeconds int    `json:"heartbeatIntervalSeconds"`
	PlaybackURL              string `json:"playbackUrl"`
}

type sessionHeartbeatResponse struct {
	SessionID    string `json:"sessionId"`
	Acknowledged bool   `json:"acknowledged"`
}

type sessionClient struct {
	baseURL      string
	apiToken     string
	capabilities playbackCapabilities
	httpClient   *http.Client
}

func newSessionClient(baseURL, apiToken string, httpClient *http.Client) *sessionClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &sessionClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiToken:     strings.TrimSpace(apiToken),
		capabilities: defaultPlaybackCapabilities(),
		httpClient:   httpClient,
	}
}

func (c *sessionClient) ready(ctx context.Context) error {
	req, err := c.request(ctx, http.MethodGet, "/readyz", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request readiness: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return responseError("readiness", resp)
	}
	return nil
}

func (c *sessionClient) resolvePlayback(ctx context.Context, serviceRef string) (string, error) {
	payload := playbackInfoRequest{
		ServiceRef:   serviceRef,
		Capabilities: c.capabilities,
	}
	var response playbackInfoResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v3/live/stream-info", payload, http.StatusOK, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.PlaybackDecisionToken) == "" {
		return "", errors.New("playback info response omitted playbackDecisionToken")
	}
	return response.PlaybackDecisionToken, nil
}

func (c *sessionClient) startSession(
	ctx context.Context,
	serviceRef string,
	decisionToken string,
	idempotencyKey string,
) (intentAcceptedResponse, error) {
	payload := intentRequest{
		Type:                  "stream.start",
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: decisionToken,
		IdempotencyKey:        idempotencyKey,
		Client:                &c.capabilities,
	}
	var response intentAcceptedResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v3/intents", payload, http.StatusAccepted, &response); err != nil {
		return intentAcceptedResponse{}, err
	}
	if strings.TrimSpace(response.SessionID) == "" || strings.TrimSpace(response.RequestID) == "" {
		return intentAcceptedResponse{}, errors.New("start intent response omitted sessionId or requestId")
	}
	if response.Status != "accepted" && response.Status != "idempotent_replay" {
		return intentAcceptedResponse{}, fmt.Errorf("unexpected start intent status %q", response.Status)
	}
	return response, nil
}

func (c *sessionClient) getSession(ctx context.Context, sessionID string) (sessionResponse, error) {
	var response sessionResponse
	path := "/api/v3/sessions/" + url.PathEscape(sessionID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &response); err != nil {
		return sessionResponse{}, err
	}
	if response.SessionID != sessionID {
		return sessionResponse{}, fmt.Errorf("session response ID %q does not match %q", response.SessionID, sessionID)
	}
	return response, nil
}

func (c *sessionClient) heartbeat(ctx context.Context, sessionID string) error {
	var response sessionHeartbeatResponse
	path := "/api/v3/sessions/" + url.PathEscape(sessionID) + "/heartbeat"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, http.StatusOK, &response); err != nil {
		return err
	}
	if response.SessionID != sessionID || !response.Acknowledged {
		return errors.New("session heartbeat was not acknowledged")
	}
	return nil
}

func (c *sessionClient) stopSession(ctx context.Context, sessionID, idempotencyKey string) error {
	payload := intentRequest{
		Type:           "stream.stop",
		SessionID:      sessionID,
		IdempotencyKey: idempotencyKey,
	}
	var response intentAcceptedResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v3/intents", payload, http.StatusAccepted, &response); err != nil {
		return err
	}
	if response.SessionID != sessionID {
		return fmt.Errorf("stop response ID %q does not match %q", response.SessionID, sessionID)
	}
	return nil
}

func (c *sessionClient) doJSON(
	ctx context.Context,
	method string,
	path string,
	payload any,
	wantStatus int,
	target any,
) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(data)
	}

	req, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		return responseError(method+" "+path, resp)
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return nil
}

func (c *sessionClient) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create %s %s request: %w", method, path, err)
	}
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}
	req.Header.Set("Accept", "application/json, application/problem+json")
	return req, nil
}

func responseError(action string, resp *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if readErr != nil {
		return fmt.Errorf("%s returned HTTP %d (read body: %v)", action, resp.StatusCode, readErr)
	}
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("%s returned HTTP %d", action, resp.StatusCode)
	}
	return fmt.Errorf("%s returned HTTP %d: %s", action, resp.StatusCode, detail)
}
