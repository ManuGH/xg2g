package jobs

import (
	"context"
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

func TestValidateConfig(t *testing.T) {
	tempDir := t.TempDir()

	testcases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "http ok", cfg: Config{OWIBase: "http://example.com", StreamPort: 8001, DataDir: tempDir}},
		{name: "https ok", cfg: Config{OWIBase: "https://example.com", StreamPort: 8001, DataDir: tempDir}},
		{name: "empty owiabase", cfg: Config{OWIBase: "", StreamPort: 8001, DataDir: tempDir}, wantErr: true},
		{name: "missing scheme", cfg: Config{OWIBase: "example.com", StreamPort: 8001, DataDir: tempDir}, wantErr: true},
		{name: "unsupported scheme", cfg: Config{OWIBase: "ftp://example.com", StreamPort: 8001, DataDir: tempDir}, wantErr: true},
		{name: "invalid port", cfg: Config{OWIBase: "http://example.com", StreamPort: -1, DataDir: tempDir}, wantErr: true},
		{name: "port too large", cfg: Config{OWIBase: "http://example.com", StreamPort: 70000, DataDir: tempDir}, wantErr: true},
		{name: "empty datadir", cfg: Config{OWIBase: "http://example.com", StreamPort: 8001, DataDir: ""}, wantErr: true},
		{name: "path traversal", cfg: Config{OWIBase: "http://example.com", StreamPort: 8001, DataDir: "../../../etc/passwd"}, wantErr: true},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfig(tc.cfg)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
