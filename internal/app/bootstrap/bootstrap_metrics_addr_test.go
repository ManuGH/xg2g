package bootstrap

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

func TestResolveMetricsAddr(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.AppConfig
		want string
	}{
		{
			name: "disabled metrics returns empty",
			cfg: config.AppConfig{
				MetricsEnabled: false,
				MetricsAddr:    "0.0.0.0:9090",
			},
			want: "",
		},
		{
			name: "enabled metrics uses configured address",
			cfg: config.AppConfig{
				MetricsEnabled: true,
				MetricsAddr:    "0.0.0.0:9000",
			},
			want: "0.0.0.0:9000",
		},
		{
			name: "enabled metrics defaults to localhost",
			cfg: config.AppConfig{
				MetricsEnabled: true,
				MetricsAddr:    "   ",
			},
			want: "127.0.0.1:9090",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMetricsAddr(tc.cfg); got != tc.want {
				t.Fatalf("resolveMetricsAddr() = %q, want %q", got, tc.want)
			}
		})
	}
}
