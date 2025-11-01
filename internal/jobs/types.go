// SPDX-License-Identifier: MIT

package jobs

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/rs/zerolog"
)

// Logger defines the logging interface for refresh operations
type Logger interface {
	Info() *zerolog.Event
	Warn() *zerolog.Event
	Error() *zerolog.Event
	Debug() *zerolog.Event
}

// OpenWebIFClient defines the interface for interacting with OpenWebIF receivers
type OpenWebIFClient interface {
	Bouquets(ctx context.Context) (map[string]string, error)
	Services(ctx context.Context, bouquetRef string) ([][]string, error)
	StreamURL(serviceRef, serviceName string) (string, error)
}

// MetricsRecorder defines the interface for recording metrics
type MetricsRecorder interface {
	RecordBouquetsCount(count int)
	RecordServicesCount(bouquet string, count int)
	RecordChannelTypeCounts(hd, sd, radio, unknown int)
	IncStreamURLBuild(status string)
	IncRefreshFailure(reason string)
	RecordXMLTV(enabled bool, channels int, err error)
	RecordPlaylistFileValidity(fileType string, valid bool)
	RecordEPGCollection(programmes, channels int, duration float64)
}

// FileWriter defines the interface for writing files atomically
type FileWriter interface {
	WriteAtomic(ctx context.Context, path string, data []byte) error
}

// Options controls the behavior of the refresh operation
type Options struct {
	Force       bool // Force refresh even if recent data exists
	IncludeEPG  bool // Include EPG data collection
	DryRun      bool // Skip actual file writes
	Parallelism int  // Max parallel workers (0 = GOMAXPROCS)
}

// Deps holds all dependencies for the refresh operation
type Deps struct {
	Logger      Logger
	Config      Config
	Client      OpenWebIFClient
	Metrics     MetricsRecorder
	FileWriter  FileWriter
	Clock       func() time.Time
	Parallelism int // Max parallel EPG fetches
}

// Artifacts represents the output of a refresh operation
type Artifacts struct {
	Bouquets      map[string]string  // Bouquet name -> reference
	Services      [][]string         // Raw services from OpenWebIF
	PlaylistItems []playlist.Item    // Processed playlist items
	EPGProgrammes int                // Number of EPG programmes collected
	Stats         RefreshStats
}

// RefreshStats contains statistics about the refresh operation
type RefreshStats struct {
	StartTime        time.Time
	EndTime          time.Time
	DurationMS       int64
	ChannelsTotal    int
	BouquetsTotal    int
	EPGProgrammes    int
	HDChannels       int
	SDChannels       int
	RadioChannels    int
	UnknownChannels  int
	Errors           []string
}

// DefaultOptions returns sensible default options
func DefaultOptions() Options {
	return Options{
		Force:       false,
		IncludeEPG:  true,
		DryRun:      false,
		Parallelism: 0, // Will use GOMAXPROCS
	}
}

// DefaultOptionsFromConfig creates options from config
func DefaultOptionsFromConfig(cfg Config) Options {
	return Options{
		Force:       false,
		IncludeEPG:  cfg.EPGEnabled,
		DryRun:      false,
		Parallelism: cfg.EPGMaxConcurrency,
	}
}
