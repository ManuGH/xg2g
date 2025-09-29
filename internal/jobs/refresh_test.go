// SPDX-License-Identifier: MIT
package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type mockOWI struct {
	bouquets map[string]string
	services map[string][][2]string
}

func (m *mockOWI) Bouquets(ctx context.Context) (map[string]string, error) {
	return m.bouquets, nil
}

func (m *mockOWI) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	return m.services[bouquetRef], nil
}

func (m *mockOWI) StreamURL(ref, name string) (string, error) {
	return "http://stream/" + ref, nil
}

func TestRefreshWithClient_Success(t *testing.T) {
	tmpdir := t.TempDir()
	cfg := Config{
		DataDir:    tmpdir,
		OWIBase:    "http://mock",
		Bouquet:    "Favourites",
		PiconBase:  "",
		XMLTVPath:  filepath.Join(tmpdir, "xmltv.xml"),
		StreamPort: 8001,
	}

	mock := &mockOWI{
		bouquets: map[string]string{"Favourites": "bref1"},
		services: map[string][][2]string{"bref1": {{"Channel A", "1:0:1"}, {"Channel B", "1:0:2"}}},
	}

	st, err := refreshWithClient(context.Background(), cfg, mock)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if st.Channels != 2 {
		t.Fatalf("expected 2 channels, got %d", st.Channels)
	}
	// check files exist
	if _, err := os.Stat(filepath.Join(tmpdir, "playlist.m3u")); err != nil {
		t.Fatalf("playlist missing: %v", err)
	}
	if _, err := os.Stat(cfg.XMLTVPath); err != nil {
		t.Fatalf("xmltv missing: %v", err)
	}
}

func TestRefreshWithClient_BouquetNotFound(t *testing.T) {
	tmpdir := t.TempDir()
	cfg := Config{DataDir: tmpdir, OWIBase: "http://mock", Bouquet: "NonExistent", StreamPort: 8001}
	mock := &mockOWI{bouquets: map[string]string{"Favourites": "bref1"}, services: map[string][][2]string{}}

	_, err := refreshWithClient(context.Background(), cfg, mock)
	if err == nil {
		t.Fatal("expected error for missing bouquet, got nil")
	}
}

func TestRefreshWithClient_ServicesError(t *testing.T) {
	tmpdir := t.TempDir()
	cfg := Config{DataDir: tmpdir, OWIBase: "http://mock", Bouquet: "Favourites", StreamPort: 8001}
	// mock that has bouquets but no services entry -> Services returns nil slice (treated as zero channels)
	mock := &mockOWI{bouquets: map[string]string{"Favourites": "bref1"}, services: map[string][][2]string{}}

	st, err := refreshWithClient(context.Background(), cfg, mock)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if st.Channels != 0 {
		t.Fatalf("expected 0 channels, got %d", st.Channels)
	}
}

func TestRefresh_InvalidStreamPort(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		OWIBase:    "http://test.local",
		DataDir:    "/tmp/test",
		StreamPort: 70000, // Invalid port
	}

	_, err := Refresh(ctx, cfg)
	if err == nil {
		t.Error("Expected error for invalid stream port")
	}
	if !errors.Is(err, ErrInvalidStreamPort) {
		t.Errorf("Expected ErrInvalidStreamPort, got: %v", err)
	}
}

func TestRefresh_ConfigValidation(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		OWIBase: "", // Invalid empty base URL
		DataDir: "/tmp/test",
	}

	_, err := Refresh(ctx, cfg)
	if err == nil {
		t.Error("Expected error for invalid config")
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "http_ok",
			cfg: Config{
				OWIBase:    "http://test.local",
				DataDir:    "/tmp",
				StreamPort: 8001,
			},
			wantErr: false,
		},
		{
			name: "https_ok",
			cfg: Config{
				OWIBase:    "https://test.local:8080",
				DataDir:    "/tmp",
				StreamPort: 8001,
			},
			wantErr: false,
		},
		{
			name: "empty_owiabase",
			cfg: Config{
				DataDir:    "/tmp",
				StreamPort: 8001,
			},
			wantErr: true,
		},
		{
			name: "missing_scheme",
			cfg: Config{
				OWIBase:    "test.local",
				DataDir:    "/tmp",
				StreamPort: 8001,
			},
			wantErr: true,
		},
		{
			name: "unsupported_scheme",
			cfg: Config{
				OWIBase:    "ftp://test.local",
				DataDir:    "/tmp",
				StreamPort: 8001,
			},
			wantErr: true,
		},
		{
			name: "invalid_port",
			cfg: Config{
				OWIBase:    "http://test.local:abc",
				DataDir:    "/tmp",
				StreamPort: 8001,
			},
			wantErr: true,
		},
		{
			name: "port_too_large",
			cfg: Config{
				OWIBase:    "http://test.local",
				DataDir:    "/tmp",
				StreamPort: 99999, // Invalid stream port
			},
			wantErr: true,
		},
		{
			name: "empty_datadir",
			cfg: Config{
				OWIBase:    "http://test.local",
				StreamPort: 8001,
			},
			wantErr: true,
		},
		{
			name: "path_traversal",
			cfg: Config{
				OWIBase:    "http://test.local",
				DataDir:    "../../../etc",
				StreamPort: 8001,
			},
			wantErr: true,
		},
		{
			name: "invalid_stream_port_zero",
			cfg: Config{
				OWIBase:    "http://test.local",
				DataDir:    "/tmp",
				StreamPort: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid_stream_port_negative",
			cfg: Config{
				OWIBase:    "http://test.local",
				DataDir:    "/tmp",
				StreamPort: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
