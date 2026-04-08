package config

import (
	"os"
	"slices"
	"testing"
)

type fakeDirEntry string

func (f fakeDirEntry) Name() string               { return string(f) }
func (f fakeDirEntry) IsDir() bool                { return false }
func (f fakeDirEntry) Type() os.FileMode          { return 0 }
func (f fakeDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestLoadAutoDetectsVAAPIDeviceWhenUnset(t *testing.T) {
	origReadDir := processReadDir
	processReadDir = func(path string) ([]os.DirEntry, error) {
		if path != "/dev/dri" {
			t.Fatalf("unexpected ReadDir path: %s", path)
		}
		return []os.DirEntry{
			fakeDirEntry("card0"),
			fakeDirEntry("renderD129"),
			fakeDirEntry("renderD128"),
		}, nil
	}
	t.Cleanup(func() { processReadDir = origReadDir })

	env := map[string]string{
		"XG2G_E2_HOST":    "http://example.com",
		"XG2G_STORE_PATH": t.TempDir(),
	}

	loader := NewLoaderWithEnv("", "test-version", vaapiLookup(env), vaapiEnviron(env))
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if got, want := cfg.FFmpeg.VaapiDevice, "/dev/dri/renderD128"; got != want {
		t.Fatalf("expected auto-detected VAAPI device %q, got %q", want, got)
	}
}

func TestLoadSkipsVAAPIAutoDetectWhenEnvExplicitlyEmpty(t *testing.T) {
	origReadDir := processReadDir
	processReadDir = func(path string) ([]os.DirEntry, error) {
		if path != "/dev/dri" {
			t.Fatalf("unexpected ReadDir path: %s", path)
		}
		return []os.DirEntry{fakeDirEntry("renderD128")}, nil
	}
	t.Cleanup(func() { processReadDir = origReadDir })

	env := map[string]string{
		"XG2G_E2_HOST":      "http://example.com",
		"XG2G_STORE_PATH":   t.TempDir(),
		"XG2G_VAAPI_DEVICE": "",
	}

	loader := NewLoaderWithEnv("", "test-version", vaapiLookup(env), vaapiEnviron(env))
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.FFmpeg.VaapiDevice != "" {
		t.Fatalf("expected explicit empty XG2G_VAAPI_DEVICE to disable auto-detect, got %q", cfg.FFmpeg.VaapiDevice)
	}
}

func TestLoadPreservesExplicitVAAPIDeviceOverride(t *testing.T) {
	origReadDir := processReadDir
	processReadDir = func(path string) ([]os.DirEntry, error) {
		if path != "/dev/dri" {
			t.Fatalf("unexpected ReadDir path: %s", path)
		}
		return []os.DirEntry{
			fakeDirEntry("renderD128"),
			fakeDirEntry("renderD129"),
		}, nil
	}
	t.Cleanup(func() { processReadDir = origReadDir })

	env := map[string]string{
		"XG2G_E2_HOST":      "http://example.com",
		"XG2G_STORE_PATH":   t.TempDir(),
		"XG2G_VAAPI_DEVICE": "/dev/dri/renderD129",
	}

	loader := NewLoaderWithEnv("", "test-version", vaapiLookup(env), vaapiEnviron(env))
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if got, want := cfg.FFmpeg.VaapiDevice, "/dev/dri/renderD129"; got != want {
		t.Fatalf("expected explicit VAAPI override %q, got %q", want, got)
	}
}

func vaapiLookup(env map[string]string) envLookupFunc {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func vaapiEnviron(env map[string]string) func() []string {
	return func() []string {
		out := make([]string, 0, len(env))
		for key, value := range env {
			out = append(out, key+"="+value)
		}
		slices.Sort(out)
		return out
	}
}
