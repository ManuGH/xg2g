package v3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

type mockPreflightReceiver struct {
	streamURL string
}

func (m mockPreflightReceiver) AddTimer(ctx context.Context, sRef string, begin, end int64, name, desc string) error {
	return nil
}

func (m mockPreflightReceiver) DeleteTimer(ctx context.Context, sRef string, begin, end int64) error {
	return nil
}

func (m mockPreflightReceiver) UpdateTimer(ctx context.Context, oldSRef string, oldBegin, oldEnd int64, newSRef string, newBegin, newEnd int64, name, description string, enabled bool) error {
	return nil
}

func (m mockPreflightReceiver) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	return nil, nil
}

func (m mockPreflightReceiver) DetectTimerChange(ctx context.Context) (openwebif.TimerChangeCap, error) {
	return openwebif.TimerChangeCap{}, nil
}

func (m mockPreflightReceiver) StreamURL(ctx context.Context, ref, name string) (string, error) {
	return m.streamURL, nil
}

func TestResolvePreflightSourceURL_AllowsReceiverURLWithUserinfo(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Network.Outbound.Enabled = true
	cfg.Network.Outbound.Allow.CIDRs = []string{"10.10.55.64/32"}
	cfg.Network.Outbound.Allow.Schemes = []string{"http"}
	cfg.Network.Outbound.Allow.Ports = []int{17999}

	deps := sessionsModuleDeps{
		cfg:  cfg,
		snap: config.BuildSnapshot(cfg, config.DefaultEnv()),
		receiver: func(cfg config.AppConfig, snap config.Snapshot) ReceiverControl {
			return mockPreflightReceiver{
				streamURL: "http://root:Kiddy99@10.10.55.64:17999/1:0:19:11:6:85:C00000:0:0:0:",
			}
		},
	}

	got, err := resolvePreflightSourceURL(context.Background(), deps, "1:0:19:11:6:85:C00000:0:0:0:")
	require.NoError(t, err)
	require.Equal(t, "http://root:Kiddy99@10.10.55.64:17999/1:0:19:11:6:85:C00000:0:0:0:", got)
}
