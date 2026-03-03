package bootstrap

import (
	"fmt"
	"os"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/daemon"
	worker "github.com/ManuGH/xg2g/internal/domain/session/manager"
	sessionports "github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/infra/bus"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/ManuGH/xg2g/internal/infra/platform"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func buildAPIConstructorDeps(cfg config.AppConfig, snap config.Snapshot, logger zerolog.Logger) api.ConstructorDeps {
	channelManager := channels.NewManager(cfg.DataDir)
	if err := channelManager.Load(); err != nil {
		logger.Error().Err(err).Msg("failed to load channel states")
	}

	seriesManager := dvr.NewManager(cfg.DataDir)
	if err := seriesManager.Load(); err != nil {
		logger.Error().Err(err).Msg("failed to load series rules")
	}

	snapshot := snap
	return api.ConstructorDeps{
		ChannelManager:      channelManager,
		SeriesManager:       seriesManager,
		Snapshot:            &snapshot,
		RecordingPathMapper: recordings.NewPathMapper(cfg.RecordingPathMappings),
	}
}

func buildMediaPipeline(cfg config.AppConfig, e2Client *enigma2.Client, logger zerolog.Logger) sessionports.MediaPipeline {
	if cfg.Engine.Mode == "virtual" {
		return stub.NewAdapter()
	}

	adapter := ffmpeg.NewLocalAdapter(
		cfg.FFmpeg.Bin,
		cfg.FFmpeg.FFprobeBin,
		cfg.HLS.Root,
		e2Client,
		logger,
		cfg.Enigma2.AnalyzeDuration,
		cfg.Enigma2.ProbeSize,
		cfg.HLS.DVRWindow,
		cfg.FFmpeg.KillTimeout,
		cfg.Enigma2.FallbackTo8001,
		cfg.Enigma2.PreflightTimeout,
		cfg.HLS.SegmentSeconds,
		cfg.Timeouts.TranscodeStart,
		cfg.Timeouts.TranscodeNoProgress,
		cfg.FFmpeg.VaapiDevice,
	)

	if cfg.FFmpeg.VaapiDevice != "" {
		if err := adapter.PreflightVAAPI(); err != nil {
			logger.Warn().Err(err).Str("device", cfg.FFmpeg.VaapiDevice).
				Msg("VAAPI preflight failed; GPU transcoding will be unavailable for sessions requesting it")
		}
	}

	return adapter
}

type v3OrchestratorFactory struct{}

func buildV3OrchestratorFactory() daemon.V3OrchestratorFactory {
	return v3OrchestratorFactory{}
}

func (v3OrchestratorFactory) Build(cfg config.AppConfig, inputs daemon.V3OrchestratorInputs) (daemon.V3Orchestrator, error) {
	if inputs.Bus == nil {
		return nil, fmt.Errorf("v3 orchestrator input bus is required")
	}
	if inputs.Store == nil {
		return nil, fmt.Errorf("v3 orchestrator input store is required")
	}
	if inputs.Pipeline == nil {
		return nil, fmt.Errorf("v3 orchestrator input pipeline is required")
	}

	host, _ := os.Hostname()
	workerOwner := fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String())

	orch := &worker.Orchestrator{
		Store:               inputs.Store,
		Bus:                 bus.NewAdapter(inputs.Bus),
		Platform:            platform.NewOSPlatform(),
		LeaseTTL:            30 * time.Second,
		HeartbeatEvery:      10 * time.Second,
		Owner:               workerOwner,
		TunerSlots:          cfg.Engine.TunerSlots,
		HLSRoot:             cfg.HLS.Root,
		PipelineStopTimeout: 5 * time.Second,
		StartConcurrency:    10,
		StopConcurrency:     10,
		Sweeper: worker.SweeperConfig{
			IdleTimeout:      cfg.Engine.IdleTimeout,
			Interval:         1 * time.Minute,
			SessionRetention: 24 * time.Hour,
		},
		OutboundPolicy: platformnet.OutboundPolicy{
			Enabled: cfg.Network.Outbound.Enabled,
			Allow: platformnet.OutboundAllowlist{
				Hosts:   append([]string(nil), cfg.Network.Outbound.Allow.Hosts...),
				CIDRs:   append([]string(nil), cfg.Network.Outbound.Allow.CIDRs...),
				Ports:   append([]int(nil), cfg.Network.Outbound.Allow.Ports...),
				Schemes: append([]string(nil), cfg.Network.Outbound.Allow.Schemes...),
			},
		},
	}
	orch.Pipeline = inputs.Pipeline
	return orch, nil
}
