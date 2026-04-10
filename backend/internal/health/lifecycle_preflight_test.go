package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestEvaluateLifecyclePreflightFatalForUnwritableStorePath(t *testing.T) {
	dataDir := t.TempDir()
	storeFile := filepath.Join(dataDir, "store.sqlite")
	if err := os.WriteFile(storeFile, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("write store file: %v", err)
	}

	report := EvaluateLifecyclePreflight(context.Background(), config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    storeFile,
		},
	}, LifecyclePreflightOptions{Operation: LifecycleOperationStartup})

	if !report.Fatal {
		t.Fatalf("expected fatal report, got %+v", report)
	}
	if report.Status != LifecyclePreflightSeverityFatal {
		t.Fatalf("expected fatal status, got %s", report.Status)
	}
}

func TestEvaluateLifecyclePreflightBlocksWhenPublicWebTruthIsNotAllowedOrigin(t *testing.T) {
	dataDir := t.TempDir()

	report := EvaluateLifecyclePreflight(context.Background(), config.AppConfig{
		DataDir:                      dataDir,
		APIListenAddr:                "127.0.0.1:8088",
		APIToken:                     "public-preflight-token-0123456789abcd",
		APIDisableLegacyTokenSources: true,
		TrustedProxies:               "127.0.0.1/32",
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
					AllowPairing:    false,
					AllowStreaming:  false,
					AllowWeb:        true,
					AllowNative:     true,
					AdvertiseReason: "public reverse proxy",
				},
			},
		},
	}, LifecyclePreflightOptions{Operation: LifecycleOperationStartup})

	if report.Fatal {
		t.Fatalf("expected non-fatal report, got %+v", report)
	}
	if !report.Blocking {
		t.Fatalf("expected blocking report, got %+v", report)
	}
	if report.Status != LifecyclePreflightSeverityBlock {
		t.Fatalf("expected block status, got %s", report.Status)
	}
}

func TestEvaluateLifecyclePreflightWarnsForMemoryBackedEngineState(t *testing.T) {
	dataDir := t.TempDir()

	report := EvaluateLifecyclePreflight(context.Background(), config.AppConfig{
		DataDir:       dataDir,
		APIListenAddr: "127.0.0.1:8088",
		Store: config.StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(dataDir, "store"),
		},
		Engine: config.EngineConfig{
			Enabled: true,
			Mode:    "virtual",
		},
		HLS: config.HLSConfig{
			Root: filepath.Join(dataDir, "hls"),
		},
	}, LifecyclePreflightOptions{Operation: LifecycleOperationStartup})

	if report.Fatal || report.Blocking {
		t.Fatalf("expected warn-only report, got %+v", report)
	}
	if report.Status != LifecyclePreflightSeverityWarn {
		t.Fatalf("expected warn status, got %s", report.Status)
	}
}
