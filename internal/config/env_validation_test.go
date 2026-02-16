package config

import (
	"strings"
	"testing"
)

func TestValidateEnvUsage_UnknownSecurityKeyFails(t *testing.T) {
	values := map[string]string{
		"XG2G_STORE_PATH":      t.TempDir(),
		"XG2G_TLS_ENFORCE_ALL": "1",
	}

	loader := NewLoaderWithEnv(
		"",
		"test",
		mapLookup(values),
		func() []string {
			return []string{"XG2G_STORE_PATH=" + values["XG2G_STORE_PATH"], "XG2G_TLS_ENFORCE_ALL=1"}
		},
	)

	err := loader.ValidateEnvUsage(true)
	if err == nil {
		t.Fatalf("expected unknown security key to fail")
	}
	if !strings.Contains(err.Error(), "XG2G_TLS_ENFORCE_ALL") {
		t.Fatalf("expected error to mention unknown key, got %v", err)
	}
}

func TestValidateEnvUsage_UnknownNonSecurityKeyWarnOnly(t *testing.T) {
	values := map[string]string{
		"XG2G_STORE_PATH":          t.TempDir(),
		"XG2G_EXPERIMENTAL_WIDGET": "on",
	}

	loader := NewLoaderWithEnv(
		"",
		"test",
		mapLookup(values),
		func() []string {
			return []string{"XG2G_STORE_PATH=" + values["XG2G_STORE_PATH"], "XG2G_EXPERIMENTAL_WIDGET=on"}
		},
	)

	if err := loader.ValidateEnvUsage(true); err != nil {
		t.Fatalf("expected unknown non-security key to warn only, got %v", err)
	}
}

func TestValidateEnvUsage_RuntimeKeyAllowed(t *testing.T) {
	values := map[string]string{
		"XG2G_STORE_PATH":        t.TempDir(),
		"XG2G_PLAYLIST_FILENAME": "playlist.custom.m3u8",
	}

	loader := NewLoaderWithEnv(
		"",
		"test",
		mapLookup(values),
		func() []string {
			return []string{
				"XG2G_STORE_PATH=" + values["XG2G_STORE_PATH"],
				"XG2G_PLAYLIST_FILENAME=" + values["XG2G_PLAYLIST_FILENAME"],
			}
		},
	)

	if err := loader.ValidateEnvUsage(true); err != nil {
		t.Fatalf("expected runtime env key to be treated as known, got %v", err)
	}
}

func mapLookup(values map[string]string) envLookupFunc {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}
