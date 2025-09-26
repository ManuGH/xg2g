package openwebif

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// /api/bouquets liefert [["<ref>","<name>"], ...]
func (c *Client) Bouquets(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/bouquets", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bouquets: %s", res.Status)
	}
	var payload struct {
		Bouquets [][]string `json:"bouquets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(payload.Bouquets))
	for _, b := range payload.Bouquets {
		if len(b) == 2 {
			ref, name := b[0], b[1]
			out[name] = ref // name -> ref
		}
	}
	return out, nil
}

// /api/getallservices?sRef=<ref> liefert Services einer Bouquet-Referenz
func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	u := c.base + "/api/getallservices?sRef=" + url.QueryEscape(bouquetRef)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("services: %s", res.Status)
	}
	var payload struct {
		Services []struct {
			ServiceName string `json:"servicename"`
			ServiceRef  string `json:"servicereference"`
		} `json:"services"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([][2]string, 0, len(payload.Services))
	for _, s := range payload.Services {
		out = append(out, [2]string{s.ServiceName, s.ServiceRef})
	}
	return out, nil
}

func StreamURL(base, ref string) string {
	return strings.TrimRight(base, "/") + ":8001/" + url.PathEscape(ref)
}
