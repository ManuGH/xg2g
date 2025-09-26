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
	Base string
	HTTP *http.Client
}

func New(base string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"),
		HTTP: &http.Client{Timeout: 15 * time.Second},
	}
}

type BouquetJSON struct {
	Bouquets []struct {
		Name    string `json:"name"`
		Bouquet string `json:"bouquet"`
	} `json:"bouquets"`
}

type ServicesJSON struct {
	Services []struct {
		ServiceName string `json:"servicename"`
		ServiceRef  string `json:"serviceref"`
	} `json:"services"`
}

func (c *Client) Bouquets(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.Base+"/api/bouquets", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("bouquets: %s", res.Status)
	}
	
	var b BouquetJSON
	if err := json.NewDecoder(res.Body).Decode(&b); err != nil {
		return nil, err
	}
	
	out := make(map[string]string)
	for _, x := range b.Bouquets {
		out[x.Name] = x.Bouquet
	}
	return out, nil
}

func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	u := c.Base + "/api/bouquet/" + url.PathEscape(bouquetRef)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("services: %s", res.Status)
	}
	
	var s ServicesJSON
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return nil, err
	}
	
	out := make([][2]string, 0, len(s.Services))
	for _, v := range s.Services {
		out = append(out, [2]string{v.ServiceName, v.ServiceRef})
	}
	return out, nil
}

func StreamURL(base, ref string) string {
	return strings.TrimRight(base, "/") + ":8001/" + url.PathEscape(ref)
}
