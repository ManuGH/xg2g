// Package main implements the xg2g-soak harness for Phase 5.3 validation.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PromClient provides Prometheus query capabilities for the harness.
type PromClient struct {
	baseURL    string
	selector   string // e.g., {job="xg2g",instance="xg2g-main"}
	httpClient *http.Client
}

// NewPromClient creates a new Prometheus client with selector.
func NewPromClient(baseURL, selector string) *PromClient {
	if selector == "" {
		selector = `{job="xg2g",instance="xg2g-main"}`
	}
	return &PromClient{
		baseURL:  baseURL,
		selector: selector,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Metric returns a metric name with the configured selector injected.
// Handles both "name" -> "name{selector}" and "name{label='val'}" -> "name{label='val',selector}"
func (p *PromClient) Metric(name string) string {
	if p.selector == "" || p.selector == "{}" {
		return name
	}
	// Strip braces from selector
	inner := p.selector
	if inner[0] == '{' {
		inner = inner[1:]
	}
	if inner[len(inner)-1] == '}' {
		inner = inner[:len(inner)-1]
	}

	// Logic:
	if idx := strings.LastIndex(name, "}"); idx != -1 {
		// Insert before closing brace
		return name[:idx] + "," + inner + "}"
	}
	return name + "{" + inner + "}"
}

// QueryValue executes an instant query and returns the scalar value.
// Returns 0 if no data or error.
func (p *PromClient) QueryValue(query string) (float64, error) {
	u, err := url.Parse(p.baseURL + "/api/v1/query")
	if err != nil {
		return 0, err
	}
	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	resp, err := p.httpClient.Get(u.String())
	if err != nil {
		return 0, err
	}
	defer func() {
		// best-effort close
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result promQueryResult
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if result.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed: %s", result.Status)
	}

	// Handle different result types
	if result.Data.ResultType == "scalar" {
		if len(result.Data.Value) >= 2 {
			if val, ok := result.Data.Value[1].(string); ok {
				var f float64
				if _, err := fmt.Sscanf(val, "%f", &f); err != nil {
					return 0, fmt.Errorf("failed to parse scalar value: %w", err)
				}
				return f, nil
			}
		}
	}

	// For vector results, take first value
	if len(result.Data.Result) == 0 {
		return 0, nil // No data
	}

	if len(result.Data.Result[0].Value) >= 2 {
		val, ok := result.Data.Result[0].Value[1].(string)
		if ok {
			var f float64
			if _, err := fmt.Sscanf(val, "%f", &f); err != nil {
				return 0, fmt.Errorf("failed to parse vector value: %w", err)
			}
			return f, nil
		}
	}

	return 0, nil
}

// QueryDelta returns increase in a counter over the given window.
func (p *PromClient) QueryDelta(metric string, window string) (float64, error) {
	query := fmt.Sprintf("increase(%s[%s])", metric, window)
	return p.QueryValue(query)
}

// QueryMax returns max value over the given window.
func (p *PromClient) QueryMax(metric string, window string) (float64, error) {
	query := fmt.Sprintf("max_over_time(%s[%s])", metric, window)
	return p.QueryValue(query)
}

// WaitForAtLeast waits until metric >= target with tolerance.
func (p *PromClient) WaitForAtLeast(query string, target float64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	tolerance := 0.01
	for time.Now().Before(deadline) {
		val, err := p.QueryValue(query)
		if err == nil && val >= target-tolerance {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	val, _ := p.QueryValue(query)
	return fmt.Errorf("condition not met: got %.2f, wanted >= %.2f", val, target)
}

// WaitForAtMost waits until metric <= target with tolerance.
func (p *PromClient) WaitForAtMost(query string, target float64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	tolerance := 0.01
	for time.Now().Before(deadline) {
		val, err := p.QueryValue(query)
		if err == nil && val <= target+tolerance {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	val, _ := p.QueryValue(query)
	return fmt.Errorf("condition not met: got %.2f, wanted <= %.2f", val, target)
}

// WaitStable waits until metric stays at target for the given duration.
func (p *PromClient) WaitStable(query string, target float64, stableFor, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	tolerance := 0.01
	stableStart := time.Time{}

	for time.Now().Before(deadline) {
		val, err := p.QueryValue(query)
		if err == nil && val >= target-tolerance && val <= target+tolerance {
			if stableStart.IsZero() {
				stableStart = time.Now()
			} else if time.Since(stableStart) >= stableFor {
				return nil
			}
		} else {
			stableStart = time.Time{} // Reset
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("metric did not stabilize at %.2f for %v", target, stableFor)
}

// promQueryResult is the Prometheus API response structure.
type promQueryResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string        `json:"resultType"`
		Value      []interface{} `json:"value"` // For scalar
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"` // For vector
	} `json:"data"`
}
