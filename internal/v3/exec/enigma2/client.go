// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package enigma2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client interacts with the Enigma2/OpenWebIf API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a new Enigma2 client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Zap requests the receiver to switch to the specified service reference.
func (c *Client) Zap(ctx context.Context, sref string) error {
	params := url.Values{}
	// NOTE: OpenWebIf API (Zap) specifically requires "sRef". "ref" causes a "parameter missing" error.
	params.Set("sRef", strings.ToUpper(sref))

	var res Response
	if err := c.get(ctx, "/api/zap", params, &res); err != nil {
		return err
	}

	if !res.Result {
		return fmt.Errorf("zap failed: %s", res.Message)
	}
	return nil
}

// GetCurrent retrieves typical current service information.
func (c *Client) GetCurrent(ctx context.Context) (*CurrentInfo, error) {
	var res CurrentInfo
	if err := c.get(ctx, "/api/getcurrent", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetSignal retrieves tuner signal stats (SNR, AGC, BER, Lock).
func (c *Client) GetSignal(ctx context.Context) (*Signal, error) {
	var res Signal
	// /api/signal usually returns directly
	if err := c.get(ctx, "/api/signal", nil, &res); err != nil {
		return nil, err
	}

	// Fallback: Some receivers (e.g. older OpenWebIF or specific images) do not return the "lock" field in JSON.
	// If Locked is false but SNR is high (e.g. > 50%), assume signal is locked.
	if !res.Locked && res.Snr > 50 {
		res.Locked = true
	}

	return &res, nil
}

func (c *Client) get(ctx context.Context, path string, params url.Values, v interface{}) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	u.Path = path
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode error: %w", err)
	}
	return nil
}
