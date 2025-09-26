package openwebif

import (
	"context"
	"encoding/json"
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

func (c *Client) Bouquets(ctx context.Context) (map[string]string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.base+"/api/bouquets", nil)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var p struct {
		Bouquets []struct {
			Name    string `json:"name"`
			Bouquet string `json:"bouquet"`
		} `json:"bouquets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, b := range p.Bouquets {
		out[b.Name] = b.Bouquet
	}
	return out, nil
}

func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	u := c.base + "/api/bouquet/" + url.PathEscape(bouquetRef)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var p struct {
		Services []struct {
			ServiceName string `json:"servicename"`
			ServiceRef  string `json:"serviceref"`
		} `json:"services"`
	}
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		return nil, err
	}
	out := make([][2]string, 0, len(p.Services))
	for _, s := range p.Services {
		out = append(out, [2]string{s.ServiceName, s.ServiceRef})
	}
	return out, nil
}

func StreamURL(base, ref string) string {
	return strings.TrimRight(base, "/") + ":8001/" + url.PathEscape(ref)
}
