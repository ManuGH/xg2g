package preflight

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

type errorRoundTripper struct {
	err error
}

func (e errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, e.err
}

type ctxWaitRoundTripper struct{}

func (ctxWaitRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

type countingRoundTripper struct {
	calls int
}

func (c *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, errors.New("unexpected network call")
}

func TestHTTPPreflightProvider_Unreachable(t *testing.T) {
	provider := NewHTTPPreflightProvider(&http.Client{
		Transport: errorRoundTripper{err: errors.New("dial error")},
	}, 0, testOutboundPolicy(t, "http://127.0.0.1"))

	res, err := provider.Check(context.Background(), SourceRef{URL: "http://127.0.0.1"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Outcome != PreflightUnreachable {
		t.Fatalf("expected outcome %q, got %q", PreflightUnreachable, res.Outcome)
	}
	if res.HTTPStatus != 0 {
		t.Fatalf("expected status 0, got %d", res.HTTPStatus)
	}
}

func TestHTTPPreflightProvider_Timeout(t *testing.T) {
	provider := NewHTTPPreflightProvider(&http.Client{
		Transport: ctxWaitRoundTripper{},
	}, 0, testOutboundPolicy(t, "http://127.0.0.1"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	res, err := provider.Check(ctx, SourceRef{URL: "http://127.0.0.1"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Outcome != PreflightTimeout {
		t.Fatalf("expected outcome %q, got %q", PreflightTimeout, res.Outcome)
	}
	if res.HTTPStatus != 0 {
		t.Fatalf("expected status 0, got %d", res.HTTPStatus)
	}
}

func TestHTTPPreflightProvider_CanceledContext(t *testing.T) {
	provider := NewHTTPPreflightProvider(&http.Client{
		Transport: ctxWaitRoundTripper{},
	}, 0, testOutboundPolicy(t, "http://127.0.0.1"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := provider.Check(ctx, SourceRef{URL: "http://127.0.0.1"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Outcome != PreflightTimeout {
		t.Fatalf("expected outcome %q, got %q", PreflightTimeout, res.Outcome)
	}
	if res.HTTPStatus != 0 {
		t.Fatalf("expected status 0, got %d", res.HTTPStatus)
	}
}

func TestHTTPPreflightProvider_StatusMapping(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		expected     PreflightOutcome
		expectStatus int
	}{
		{"ok", http.StatusOK, PreflightOK, http.StatusOK},
		{"unauthorized", http.StatusUnauthorized, PreflightUnauthorized, http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden, PreflightForbidden, http.StatusForbidden},
		{"not_found", http.StatusNotFound, PreflightNotFound, http.StatusNotFound},
		{"bad_gateway", http.StatusBadGateway, PreflightBadGateway, http.StatusBadGateway},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			provider := NewHTTPPreflightProvider(srv.Client(), 0, testOutboundPolicy(t, srv.URL))
			res, err := provider.Check(context.Background(), SourceRef{URL: srv.URL})
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if res.Outcome != tc.expected {
				t.Fatalf("expected outcome %q, got %q", tc.expected, res.Outcome)
			}
			if res.HTTPStatus != tc.expectStatus {
				t.Fatalf("expected status %d, got %d", tc.expectStatus, res.HTTPStatus)
			}
		})
	}
}

func TestHTTPPreflightProvider_NoRedirect(t *testing.T) {
	redirected := false
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	provider := NewHTTPPreflightProvider(srv.Client(), 0, testOutboundPolicy(t, srv.URL))
	res, err := provider.Check(context.Background(), SourceRef{URL: srv.URL})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if redirected {
		t.Fatal("expected no redirect follow")
	}
	if res.Outcome != PreflightInternal {
		t.Fatalf("expected outcome %q, got %q", PreflightInternal, res.Outcome)
	}
	if res.HTTPStatus != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, res.HTTPStatus)
	}
}

func TestHTTPPreflightProvider_RejectsDisallowedURLBeforeRequest(t *testing.T) {
	transport := &countingRoundTripper{}
	provider := NewHTTPPreflightProvider(&http.Client{
		Transport: transport,
	}, 0, platformnet.OutboundPolicy{
		Enabled: true,
		Allow: platformnet.OutboundAllowlist{
			Hosts:   []string{"example.com"},
			Ports:   []int{443},
			Schemes: []string{"https"},
		},
	})

	res, err := provider.Check(context.Background(), SourceRef{URL: "http://127.0.0.1"})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if res.Outcome != PreflightInternal {
		t.Fatalf("expected outcome %q, got %q", PreflightInternal, res.Outcome)
	}
	if transport.calls != 0 {
		t.Fatalf("expected no network call, got %d", transport.calls)
	}
}

func testOutboundPolicy(t *testing.T, rawURL string) platformnet.OutboundPolicy {
	t.Helper()

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test url: %v", err)
	}
	host := u.Hostname()

	port := 0
	switch u.Scheme {
	case "http":
		port = 80
	case "https":
		port = 443
	}
	if u.Port() != "" {
		var parseErr error
		port, parseErr = parsePort(u.Port())
		if parseErr != nil {
			t.Fatalf("parse port: %v", parseErr)
		}
	}

	return platformnet.OutboundPolicy{
		Enabled: true,
		Allow: platformnet.OutboundAllowlist{
			Hosts:   []string{host},
			CIDRs:   testAllowedCIDRs(host),
			Ports:   []int{port},
			Schemes: []string{u.Scheme},
		},
	}
}

func parsePort(raw string) (int, error) {
	if raw == "" {
		return 0, errors.New("empty port")
	}
	return strconv.Atoi(raw)
}

func testAllowedCIDRs(host string) []string {
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}
	}
	return nil
}

func TestHTTPPreflightProvider_SetsBasicAuthFromSourceRef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Fatal("expected basic auth on preflight request")
		}
		if username != "root" || password != "secret" {
			t.Fatalf("unexpected basic auth credentials: %q / %q", username, password)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	provider := NewHTTPPreflightProvider(srv.Client(), 0, testOutboundPolicy(t, srv.URL))
	res, err := provider.Check(context.Background(), SourceRef{
		URL:      srv.URL + "/stream?" + url.Values{"ref": []string{"1:0:1:"}}.Encode(),
		Username: "root",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.Outcome != PreflightOK {
		t.Fatalf("expected outcome %q, got %q", PreflightOK, res.Outcome)
	}
}
