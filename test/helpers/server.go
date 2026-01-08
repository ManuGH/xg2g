// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package helpers provides common test utilities for integration and unit tests.
// Following Go 2025 best practices, all helper functions use t.Helper() to
// ensure proper error reporting in test output.
package helpers

import (
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
)

// TestServerOptions configures the test server setup
type TestServerOptions struct {
	DataDir    string
	OWIBase    string
	StreamPort int
	APIToken   string
	Bouquet    string
}

// TestServer wraps a test HTTP server with its configuration
type TestServer struct {
	Server *httptest.Server
	Config config.AppConfig
	API    *api.Server
}

// Close closes the test server and cleans up resources
func (ts *TestServer) Close() {
	if ts.Server != nil {
		ts.Server.Close()
	}
}

// NewTestServer creates a new test HTTP server with the given configuration.
// It automatically marks the calling function as a test helper.
//
// Usage:
//
//	ts := helpers.NewTestServer(t, helpers.TestServerOptions{
//	    DataDir: t.TempDir(),
//	    APIToken: "test-token",
//	})
//	defer ts.Close()
func NewTestServer(t *testing.T, opts TestServerOptions) *TestServer {
	t.Helper()

	// Apply defaults
	if opts.OWIBase == "" {
		opts.OWIBase = "http://test.local"
	}
	if opts.StreamPort == 0 {
		opts.StreamPort = 8001
	}
	if opts.Bouquet == "" {
		opts.Bouquet = "Favourites"
	}

	cfg := config.AppConfig{
		DataDir: opts.DataDir,
		Enigma2: config.Enigma2Settings{
			BaseURL:    opts.OWIBase,
			StreamPort: opts.StreamPort,
		},
		APIToken: opts.APIToken,
		Bouquet:  opts.Bouquet,
	}

	apiServer := api.New(cfg, nil)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)

	return &TestServer{
		Server: testServer,
		Config: cfg,
		API:    apiServer,
	}
}

// NewTestServerWithConfig creates a test server from an existing config.
// Use this when you need full control over the config structure.
func NewTestServerWithConfig(t *testing.T, cfg config.AppConfig) *TestServer {
	t.Helper()

	apiServer := api.New(cfg, nil)
	handler := apiServer.Handler()
	testServer := httptest.NewServer(handler)

	return &TestServer{
		Server: testServer,
		Config: cfg,
		API:    apiServer,
	}
}
