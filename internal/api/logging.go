package api

import (
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

// logger returns a context-aware logger configured with component metadata.
func logger(component string) *zerolog.Logger {
	l := xglog.WithComponent(component)
	return &l
}
