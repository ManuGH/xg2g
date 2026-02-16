package api

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/channels"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/dvr"
	"github.com/ManuGH/xg2g/internal/recordings"
	"github.com/stretchr/testify/require"
)

func TestNewWithDeps_UsesInjectedConstructorDeps(t *testing.T) {
	cfg := config.AppConfig{
		DataDir: t.TempDir(),
	}
	cfgMgr := config.NewManager("")

	channelManager := channels.NewManager(cfg.DataDir)
	seriesManager := dvr.NewManager(cfg.DataDir)
	pathMapper := recordings.NewPathMapper(cfg.RecordingPathMappings)
	snap := config.BuildSnapshot(cfg, config.DefaultEnv())
	snap.Runtime.PlaylistFilename = "deps-playlist.m3u"

	s, err := NewWithDeps(cfg, cfgMgr, ConstructorDeps{
		ChannelManager:      channelManager,
		SeriesManager:       seriesManager,
		Snapshot:            &snap,
		RecordingPathMapper: pathMapper,
	})
	require.NoError(t, err)
	require.Same(t, channelManager, s.channelManager)
	require.Same(t, seriesManager, s.seriesManager)
	require.Same(t, pathMapper, s.recordingPathMapper)
	require.Equal(t, "deps-playlist.m3u", s.snap.Runtime.PlaylistFilename)
}
