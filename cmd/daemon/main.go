package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/jobs"
)

// wird via -ldflags gesetzt (siehe Dockerfile)
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

	log.Printf("Starting xg2g v%s on %s", Version, addr)
	log.Printf("Config: data=%s, owi=%s, bouquet=%s, xmltv=%s, fuzzy=%d",
		cfg.DataDir, cfg.OWIBase, cfg.Bouquet, cfg.XMLTVPath, cfg.FuzzyMax)

	log.Fatal(http.ListenAndServe(addr, s.Handler()))
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
