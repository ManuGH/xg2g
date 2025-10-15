// SPDX-License-Identifier: MIT
package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
)

// OwiClient defines the interface for OpenWebIF operations, allowing for mocks in tests.
type OwiClient interface {
	Bouquets(ctx context.Context) (map[string]string, error)
	Services(ctx context.Context, bouquetRef string) ([][2]string, error)
	StreamURL(ref, name string) (string, error)
	GetEPG(ctx context.Context, sRef string, days int) ([]openwebif.EPGEvent, error)
}

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

func (m *mockOWI) GetEPG(ctx context.Context, sRef string, days int) ([]openwebif.EPGEvent, error) {
	// Return mock EPG data for tests
	return []openwebif.EPGEvent{
		{
			ID:          1,
			Title:       "Test Programme",
			Description: "Test Description",
			Begin:       time.Now().Unix(),
			Duration:    3600,
			SRef:        sRef,
		},
	}, nil
}

// refreshWithClient is a test helper that allows injecting a mock client.
func refreshWithClient(ctx context.Context, cfg Config, cl OwiClient) (*Status, error) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")
	logger.Info().Str("event", "refresh.start").Msg("starting refresh")

	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	bqs, err := cl.Bouquets(ctx)
	if err != nil {
		return nil, fmt.Errorf("bouquets: %w", err)
	}

	ref, ok := bqs[cfg.Bouquet]
	if !ok {
		return nil, fmt.Errorf("bouquet %q not found", cfg.Bouquet)
	}

	services, err := cl.Services(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("services for bouquet %q: %w", cfg.Bouquet, err)
	}

	items := make([]playlist.Item, 0, len(services))
	for i, svc := range services {
		if len(svc) < 2 {
			continue
		}
		name, sref := svc[0], svc[1]

		item := playlist.Item{
			Name:    name,
			TvgID:   makeStableIDFromSRef(sref),
			TvgChNo: i + 1,
			Group:   cfg.Bouquet,
		}

		streamURL, err := cl.StreamURL(sref, name)
		if err != nil {
			return nil, fmt.Errorf("stream url for %q: %w", name, err)
		}
		item.URL = streamURL

		if cfg.PiconBase != "" {
			item.TvgLogo = strings.TrimRight(cfg.PiconBase, "/") + "/" + url.PathEscape(sref) + ".png"
		}

		items = append(items, item)
	}

	playlistPath := filepath.Join(cfg.DataDir, "playlist.m3u")
	if err := writeM3U(ctx, playlistPath, items); err != nil {
		return nil, fmt.Errorf("failed to write M3U playlist: %w", err)
	}
	logger.Info().
		Str("event", "playlist.write").
		Str("path", playlistPath).
		Int("channels", len(items)).
		Msg("playlist written")

	if cfg.XMLTVPath != "" {
		xmlCh := make([]epg.Channel, 0, len(items))
		for _, it := range items {
			ch := epg.Channel{ID: it.TvgID, DisplayName: []string{it.Name}}
			if it.TvgLogo != "" {
				ch.Icon = &epg.Icon{Src: it.TvgLogo}
			}
			xmlCh = append(xmlCh, ch)
		}
		if err := epg.WriteXMLTV(xmlCh, filepath.Join(cfg.DataDir, cfg.XMLTVPath)); err != nil {
			logger.Warn().
				Err(err).
				Str("event", "xmltv.failed").
				Str("path", cfg.XMLTVPath).
				Int("channels", len(xmlCh)).
				Msg("XMLTV generation failed")
		} else {
			logger.Info().
				Str("event", "xmltv.success").
				Str("path", cfg.XMLTVPath).
				Int("channels", len(xmlCh)).
				Msg("XMLTV generated")
		}
	}

	status := &Status{LastRun: time.Now(), Channels: len(items)}
	logger.Info().
		Str("event", "refresh.success").
		Int("channels", status.Channels).
		Msg("refresh completed")
	return status, nil
}

func TestRefreshWithClient_Success(t *testing.T) {
	tmpdir := t.TempDir()
	cfg := Config{
		DataDir:    tmpdir,
		OWIBase:    "http://mock",
		Bouquet:    "Favourites",
		PiconBase:  "",
		XMLTVPath:  "xmltv.xml", // Use relative path, not absolute
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
	if _, err := os.Stat(filepath.Join(tmpdir, "xmltv.xml")); err != nil {
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

func TestRefresh_M3UWriteError(t *testing.T) {
	// Create a read-only directory to force a write error
	tmpdir := t.TempDir()
	readonlyDir := filepath.Join(tmpdir, "readonly")
	if err := os.Mkdir(readonlyDir, 0555); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}

	cfg := Config{
		DataDir:    readonlyDir,
		OWIBase:    "http://mock",
		Bouquet:    "Favourites",
		StreamPort: 8001,
	}

	mock := &mockOWI{
		bouquets: map[string]string{"Favourites": "bref1"},
		services: map[string][][2]string{"bref1": {{"Channel A", "1:0:1"}}},
	}

	_, err := refreshWithClient(context.Background(), cfg, mock)
	if err == nil {
		t.Fatal("expected an error when writing M3U to a read-only directory, got nil")
	}
	if !strings.Contains(err.Error(), "failed to write M3U playlist") {
		t.Errorf("expected error to contain 'failed to write M3U playlist', got: %v", err)
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
		{
			name: "missing_host",
			cfg: Config{
				OWIBase:    "http:///", // scheme present but host missing
				DataDir:    "/tmp",
				StreamPort: 8001,
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

func TestMakeStableIDFromSRef(t *testing.T) {
	a := makeStableIDFromSRef("1:0:19:132F:3EF:1:C00000:0:0:0:")
	b := makeStableIDFromSRef("1:0:19:132F:3EF:1:C00000:0:0:0:")
	c := makeStableIDFromSRef("1:0:19:1334:3EF:1:C00000:0:0:0:")
	if a != b {
		t.Fatalf("stable ID should be deterministic; got %q and %q", a, b)
	}
	if a == c {
		t.Fatalf("different sRefs should yield different IDs; got %q == %q", a, c)
	}
	if wantPrefix := "sref-"; len(a) < len(wantPrefix) || a[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("stable ID must be prefixed with %q; got %q", wantPrefix, a)
	}
}
