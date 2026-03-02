package v3

import (
	"context"
	"net/http"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/log"
)

// requestID returns the Request ID from the context.
// STRICT: It must check for empty ID and log/panic in strict dev mode,
// but for production robustness we return empty string and rely on Contract Tests to fail.
func requestID(ctx context.Context) string {
	id := log.RequestIDFromContext(ctx)
	// Note: We do not auto-generate here. Middleware must own generation.
	// Contract Tests must assert id != "" to verify Middleware wiring.
	return id
}

// ensureTraceHeader ensures the response header X-Request-Id is set from the context.
// STRICT: Must use controlhttp.HeaderRequestID constant.
func ensureTraceHeader(w http.ResponseWriter, ctx context.Context) {
	if w.Header().Get(controlhttp.HeaderRequestID) == "" {
		if id := requestID(ctx); id != "" {
			w.Header().Set(controlhttp.HeaderRequestID, id)
		}
	}
}
