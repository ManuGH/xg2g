package v3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

type preflightReceiverAdapter struct {
	*openwebif.Client
}

func TestResolvePreflightSource_UsesReceiverAuthWithoutEmbeddingUserinfo(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Enigma2.BaseURL = "http://10.10.55.64"
	cfg.Enigma2.StreamPort = 17999
	cfg.Enigma2.Username = "root"
	cfg.Enigma2.Password = "Kiddy99"
	cfg.Network.Outbound.Enabled = true
	cfg.Network.Outbound.Allow.CIDRs = []string{"10.10.55.64/32"}
	cfg.Network.Outbound.Allow.Schemes = []string{"http"}
	cfg.Network.Outbound.Allow.Ports = []int{17999}

	deps := sessionsModuleDeps{
		cfg:  cfg,
		snap: config.BuildSnapshot(cfg, config.DefaultEnv()),
		receiver: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
			return preflightReceiverAdapter{
				Client: openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, openwebif.Options{
					Username: cfg.Enigma2.Username,
					Password: cfg.Enigma2.Password,
				}),
			}
		},
	}

	got, err := resolvePreflightSource(context.Background(), deps, "1:0:19:11:6:85:C00000:0:0:0:")
	require.NoError(t, err)
	require.Equal(t, "http://10.10.55.64:17999/1:0:19:11:6:85:C00000:0:0:0:", got.URL)
	require.Equal(t, "root", got.Username)
	require.Equal(t, "Kiddy99", got.Password)
}
