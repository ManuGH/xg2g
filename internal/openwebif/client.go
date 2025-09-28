package openwebif

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	base string
	port int
	http *http.Client
}

// ClientInterface defines the subset used by other packages and tests.
type ClientInterface interface {
	Bouquets(ctx context.Context) (map[string]string, error)
	Services(ctx context.Context, bouquetRef string) ([][2]string, error)
	StreamURL(ref string) string
}

func New(base string) *Client {
	// Default-Streamport
	port := 8001
	if v := os.Getenv("XG2G_STREAM_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			port = p
		}
	}
	return &Client{
		base: strings.TrimRight(base, "/"),
		port: port,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}
func (c *Client) Bouquets(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/bouquets", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			// ignore close errors (best-effort)
			_ = err
		}
	}()
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
			out[b[1]] = b[0]
		} // name -> ref
	}
	return out, nil
}

// /api/bouquets: [["<ref>","<name>"], ...]

type svcPayload struct {
	Services []struct {
		ServiceName string `json:"servicename"`
		ServiceRef  string `json:"servicereference"`
	} `json:"services"`
}

func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	try := func(urlPath string) ([][2]string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+urlPath, nil)
		if err != nil {
			return nil, err
		}
		res, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := res.Body.Close(); err != nil {
				_ = err
			}
		}()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("services: %s", res.Status)
		}

		var p svcPayload
		if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
			return nil, err
		}
		out := make([][2]string, 0, len(p.Services))
		for _, s := range p.Services {
			// 1:7:* = Bouquet-Container; 1:0:* = TV/Radio Services
			if strings.HasPrefix(s.ServiceRef, "1:7:") {
				continue
			}
			out = append(out, [2]string{s.ServiceName, s.ServiceRef})
		}
		return out, nil
	}

	// 1) bevorzugt: flach
	if out, err := try("/api/getallservices?bRef=" + url.QueryEscape(bouquetRef)); err == nil && len(out) > 0 {
		return out, nil
	}
	// 2) Fallback: Services des Bouquets
	if out, err := try("/api/getservices?sRef=" + url.QueryEscape(bouquetRef)); err == nil && len(out) > 0 {
		return out, nil
	}
	// 3) fallback: return empty list if nothing found
	return [][2]string{}, nil
}

func (c *Client) StreamURL(ref string) string {
	return fmt.Sprintf("%s:%d/%s", c.base, c.port, url.PathEscape(ref))
}

// Beibehaltener Helfer (nutzt jetzt New, damit ENV wirkt)
func StreamURL(base, ref string) string {
	return New(base).StreamURL(ref)
}
