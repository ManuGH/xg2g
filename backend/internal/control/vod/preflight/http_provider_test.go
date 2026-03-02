package preflight

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestHTTPPreflightProvider_Unreachable(t *testing.T) {
	provider := NewHTTPPreflightProvider(&http.Client{
		Transport: errorRoundTripper{err: errors.New("dial error")},
	}, 0)

	res, err := provider.Check(context.Background(), SourceRef{URL: "http://example.invalid"})
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
	}, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	res, err := provider.Check(ctx, SourceRef{URL: "http://example.invalid"})
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
	}, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := provider.Check(ctx, SourceRef{URL: "http://example.invalid"})
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

			provider := NewHTTPPreflightProvider(srv.Client(), 0)
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

	provider := NewHTTPPreflightProvider(srv.Client(), 0)
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
