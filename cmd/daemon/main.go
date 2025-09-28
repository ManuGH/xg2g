package main

import (
	"net/http"
	"os"
	"strconv"

	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var Version = "dev"

func main() {
	cfg := jobs.Config{
		DataDir:   env("XG2G_DATA", "/data"),
		OWIBase:   env("XG2G_OWI_BASE", "http://10.10.55.57"),
		Bouquet:   env("XG2G_BOUQUET", "Premium"),
		PiconBase: env("XG2G_PICON_BASE", ""),
		XMLTVPath: env("XG2G_XMLTV", ""),
		FuzzyMax:  atoi(env("XG2G_FUZZY_MAX", "2")),
	}

	s := api.New(cfg)
	addr := env("XG2G_LISTEN", ":34400")

	// Configure zerolog to use RFC3339 timestamps for readability
	zerolog.TimeFieldFormat = time.RFC3339
	log.Info().Str("version", Version).Str("addr", addr).Msg("starting xg2g")
	log.Info().Str("data", cfg.DataDir).Str("owi", cfg.OWIBase).Str("bouquet", cfg.Bouquet).
		Str("xmltv", cfg.XMLTVPath).Int("fuzzy", cfg.FuzzyMax).Str("picon", cfg.PiconBase).
		Msg("config")

	if err := http.ListenAndServe(addr, s.Handler()); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return 0
}
