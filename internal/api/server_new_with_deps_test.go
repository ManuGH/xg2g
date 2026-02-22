package api

import (
	"os"
	"path/filepath"
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

func TestNewWithDeps_FailsWhenChannelStateLoadFails(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "channels.json"), []byte("{not-json"), 0600))

	cfg := config.AppConfig{DataDir: dataDir}
	cfgMgr := config.NewManager("")

	_, err := NewWithDeps(cfg, cfgMgr, ConstructorDeps{})
	require.Error(t, err)
	require.ErrorContains(t, err, "load channel states")
}

func TestNewWithDeps_FailsWhenSeriesRulesLoadFails(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "channels.json"), []byte("[]"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "series_rules.json"), []byte("{not-json"), 0600))

	cfg := config.AppConfig{DataDir: dataDir}
	cfgMgr := config.NewManager("")

	_, err := NewWithDeps(cfg, cfgMgr, ConstructorDeps{})
	require.Error(t, err)
	require.ErrorContains(t, err, "load series rules")
}
