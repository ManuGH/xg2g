// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package legacy

import (
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/hdhr"
)

// Runtime defines the runtime dependencies required by legacy HTTP handlers.
type Runtime interface {
	CurrentConfig() config.AppConfig
	PlaylistFilename() string
	ResolveDataFilePath(rel string) (string, error)
	HDHomeRunServer() *hdhr.Server
	PiconSemaphore() chan struct{}
}
