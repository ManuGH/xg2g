package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestPerformStartupChecksFailsWhenStorePathIsNotDirectory(t *testing.T) {
	dataDir := t.TempDir()
	storeFile := filepath.Join(dataDir, "store.sqlite")
	if err := os.WriteFile(storeFile, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("write store file: %v", err)
	}

	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storeFile,
		},
		Engine: config.EngineConfig{
			Enabled: true,
			Mode:    "virtual",
		},
		HLS: config.HLSConfig{
			Root: filepath.Join(dataDir, "hls"),
		},
	}

	if err := PerformStartupChecks(context.Background(), cfg); err == nil {
		t.Fatal("expected startup checks to fail for non-directory store path")
	}
}

func TestPerformStartupChecksFailsWhenRecordingRootIsNotWritableDirectory(t *testing.T) {
	dataDir := t.TempDir()
	recordingRoot := filepath.Join(dataDir, "recordings.json")
	if err := os.WriteFile(recordingRoot, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("write recording root file: %v", err)
	}

	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(dataDir, "store"),
		},
		Engine: config.EngineConfig{
			Enabled: false,
			Mode:    "virtual",
		},
		RecordingRoots: map[string]string{
			"main": recordingRoot,
		},
	}

	if err := PerformStartupChecks(context.Background(), cfg); err == nil {
		t.Fatal("expected startup checks to fail for non-directory recording root")
	}
}

func TestPerformStartupChecksFailsWhenPublicConnectivityContractIsFatal(t *testing.T) {
	dataDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(dataDir, "store"),
		},
		Connectivity: config.ConnectivityConfig{
			Profile: "reverse_proxy",
			PublishedEndpoints: []config.PublishedEndpointConfig{
				{
					URL:             "https://public.example",
					Kind:            "public_https",
					Priority:        10,
					AllowPairing:    true,
					AllowStreaming:  true,
					AllowWeb:        true,
					AllowNative:     true,
					AdvertiseReason: "public reverse proxy",
				},
			},
		},
	}

	err := PerformStartupChecks(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected startup checks to fail for fatal public deployment contract")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "trustedProxies") {
		t.Fatalf("expected trusted proxies startup failure, got %v", err)
	}
}

func TestPerformStartupChecksFailsWhenPublicExposureSecurityContractIsFatal(t *testing.T) {
	dataDir := t.TempDir()
	cfg := config.AppConfig{
		DataDir:         dataDir,
		APIListenAddr:   "127.0.0.1:8088",
		TrustedProxies:  "127.0.0.1/32",
		AllowedOrigins:  []string{"https://public.example"},
		RateLimitAuth:   10,
		RateLimitGlobal: 100,
		Store: config.StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(dataDir, "store"),
		},
		Connectivity: config.ConnectivityConfig{
			Profile: "reverse_proxy",
			PublishedEndpoints: []config.PublishedEndpointConfig{
				{
					URL:             "https://public.example",
					Kind:            "public_https",
					Priority:        10,
					AllowPairing:    true,
					AllowStreaming:  true,
					AllowWeb:        true,
					AllowNative:     true,
					AdvertiseReason: "public reverse proxy",
				},
			},
		},
	}

	err := PerformStartupChecks(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected startup checks to fail for public exposure security contract")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "public exposure security contract") || !strings.Contains(got, "APIToken") {
		t.Fatalf("expected public exposure startup failure, got %v", err)
	}
}
