package api

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	// Configure zerolog
	zerolog.TimeFieldFormat = time.RFC3339
	// Set global log level from environment if provided
	if lvlStr := os.Getenv("LOG_LEVEL"); lvlStr != "" {
		if lvl, err := zerolog.ParseLevel(lvlStr); err == nil {
			zerolog.SetGlobalLevel(lvl)
		}
	}
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	})
}

// Logger returns a context-aware logger configured with component metadata
//
//nolint:unused
func logger(component string) *zerolog.Logger {
	logger := log.With().
		Str("component", component).
		Str("version", os.Getenv("VERSION")).
		Logger()
	return &logger
}
