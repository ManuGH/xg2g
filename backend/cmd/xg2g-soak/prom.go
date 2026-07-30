package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type promClient struct {
	baseURL    string
	selector   string
	httpClient *http.Client
}

type promQueryResult struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Value      []any  `json:"value"`
		Result     []struct {
			Value []any `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func newPromClient(baseURL, selector string, httpClient *http.Client) *promClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &promClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		selector:   strings.TrimSpace(selector),
		httpClient: httpClient,
	}
}

func (p *promClient) metric(name string) (string, error) {
	if p.selector == "" || p.selector == "{}" {
		return name, nil
	}
	if !strings.HasPrefix(p.selector, "{") || !strings.HasSuffix(p.selector, "}") {
		return "", errors.New("prometheus selector must be enclosed in braces")
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(p.selector, "{"), "}"))
	if inner == "" {
		return name, nil
	}
	if idx := strings.LastIndex(name, "}"); idx >= 0 {
		return name[:idx] + "," + inner + "}", nil
	}
	return name + "{" + inner + "}", nil
}

func (p *promClient) queryValue(ctx context.Context, query string) (float64, error) {
	endpoint, err := url.Parse(p.baseURL + "/api/v1/query")
	if err != nil {
		return 0, fmt.Errorf("parse prometheus URL: %w", err)
	}
	values := endpoint.Query()
	values.Set("query", query)
	endpoint.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("create prometheus request: %w", err)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("query prometheus: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, responseError("Prometheus query", resp)
	}

	var result promQueryResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode prometheus response: %w", err)
	}
	if result.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed (%s): %s", result.ErrorType, result.Error)
	}

	switch result.Data.ResultType {
	case "scalar":
		return parsePromValue(result.Data.Value)
	case "vector":
		if len(result.Data.Result) != 1 {
			return 0, fmt.Errorf("prometheus query returned %d series, expected exactly one", len(result.Data.Result))
		}
		return parsePromValue(result.Data.Result[0].Value)
	default:
		return 0, fmt.Errorf("unsupported prometheus result type %q", result.Data.ResultType)
	}
}

func parsePromValue(value []any) (float64, error) {
	if len(value) < 2 {
		return 0, errors.New("prometheus value is missing")
	}
	raw, ok := value[1].(string)
	if !ok {
		return 0, errors.New("prometheus value is not a string")
	}
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse prometheus value %q: %w", raw, err)
	}
	return number, nil
}

func (p *promClient) targetUp(ctx context.Context) error {
	metric, err := p.metric("up")
	if err != nil {
		return err
	}
	value, err := p.queryValue(ctx, metric)
	if err != nil {
		return err
	}
	if value != 1 {
		return fmt.Errorf("prometheus target up value is %.0f, expected 1", value)
	}
	return nil
}
