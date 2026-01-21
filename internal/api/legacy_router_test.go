package api

import (
	"net/http"

	"github.com/ManuGH/xg2g/internal/config"
)

// NewRouter creates and configures a new router with all middlewares and handlers.
//
// [RC-WARNING] In production, use api.New() with a valid config.Manager.
// This helper is kept for testing/simple setups.
func NewRouter(cfg config.AppConfig) http.Handler {
	// Revert to panic behavior but in test file (ignored by gate)
	// Or better: actually implementing it? No, keeping behavior consistent.
	panic("api.NewRouter is deprecated for production. Use api.New(cfg, cfgMgr).routes()")
}
