// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/stretchr/testify/require"
)

// recordingPipeline records whether it was started and with which source, so a
// test can assert either that the guard stopped a start or that it let one
// through.
type recordingPipeline struct {
	started bool
	source  string
}

func (p *recordingPipeline) Start(_ context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	p.started = true
	p.source = spec.Source.ID
	return "handle", nil
}

func (p *recordingPipeline) Stop(context.Context, ports.RunHandle) error { return nil }

func (p *recordingPipeline) Health(context.Context, ports.RunHandle) ports.HealthStatus {
	return ports.HealthStatus{}
}

func guardOrchestrator(t *testing.T, receiverBaseURL string) (*Orchestrator, *recordingPipeline) {
	t.Helper()
	pipeline := &recordingPipeline{}
	return &Orchestrator{
		Store:           store.NewMemoryStore(),
		Pipeline:        pipeline,
		ReceiverBaseURL: receiverBaseURL,
		OutboundPolicy: platformnet.OutboundPolicy{
			// Deliberately permissive: without it the outbound check would refuse
			// these URLs on its own and the test would pass for the wrong reason.
			Enabled: true,
			Allow: platformnet.OutboundAllowlist{
				Hosts:   []string{"192.168.1.50", "203.0.113.10"},
				CIDRs:   []string{"192.168.0.0/16", "203.0.113.0/24"},
				Schemes: []string{"http", "https"},
				Ports:   []int{80, 443, 8001, 8002},
			},
		},
	}, pipeline
}

// A direct URL aimed at the receiver must never become a session. It would reach
// ffmpeg as an -i argument with no tuner lease, no zap and no readiness check -
// the architecture break shared ingest exists to remove, under a source class
// meant for external IPTV.
func TestStartPipeline_RejectsReceiverURLAsSource(t *testing.T) {
	receiverURLs := []string{
		"http://192.168.1.50:8001/1:0:19:83:6:85:C00000:0:0:0:",
		"http://192.168.1.50:8002/1:0:19:83:6:85:C00000:0:0:0:",
		"http://192.168.1.50/web/stream.m3u",
	}

	for _, ref := range receiverURLs {
		t.Run(ref, func(t *testing.T) {
			orch, pipeline := guardOrchestrator(t, "http://192.168.1.50:8080")

			_, _, err := orch.startPipeline(
				context.Background(),
				model.StartSessionEvent{SessionID: "sess-guard"},
				&sessionContext{SessionID: "sess-guard", ServiceRef: ref, Mode: model.ModeLive},
				model.ProfileSpec{Name: "compatible"},
				0,
				startupAttempt{},
			)

			require.Error(t, err, "a receiver URL must not start a session")
			require.False(t, pipeline.started, "pipeline was started with %q", pipeline.source)
			require.Contains(t, strings.ToLower(err.Error()), "receiver")
		})
	}
}

// The guard must not cost the feature it shares a code path with: a genuine
// external IPTV URL still starts.
func TestStartPipeline_AllowsExternalURLSource(t *testing.T) {
	orch, pipeline := guardOrchestrator(t, "http://192.168.1.50:8080")

	_, _, err := orch.startPipeline(
		context.Background(),
		model.StartSessionEvent{SessionID: "sess-iptv"},
		&sessionContext{
			SessionID:  "sess-iptv",
			ServiceRef: "http://203.0.113.10:8001/live/channel.m3u8",
			Mode:       model.ModeLive,
		},
		model.ProfileSpec{Name: "compatible"},
		0,
		startupAttempt{},
	)

	require.NoError(t, err)
	require.True(t, pipeline.started, "external IPTV must still reach the pipeline")
}

// With no receiver configured the guard stands down rather than refusing
// everything.
func TestStartPipeline_GuardInertWithoutConfiguredReceiver(t *testing.T) {
	orch, pipeline := guardOrchestrator(t, "")

	_, _, err := orch.startPipeline(
		context.Background(),
		model.StartSessionEvent{SessionID: "sess-noreceiver"},
		&sessionContext{
			SessionID:  "sess-noreceiver",
			ServiceRef: "http://192.168.1.50:8001/1:0:19:83:6:85:C00000:0:0:0:",
			Mode:       model.ModeLive,
		},
		model.ProfileSpec{Name: "compatible"},
		0,
		startupAttempt{},
	)

	require.NoError(t, err)
	require.True(t, pipeline.started)
}
