package recordings

import (
	"context"
	"errors"
	"net/http"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

// DeleteDeps defines dependencies for the Delete handler.
type DeleteDeps struct {
	NewOWIClient func() OpenWebIFClient
	WriteProblem func(w http.ResponseWriter, r *http.Request, status int, typ, title, code, detail string)
	Logger       func(msg string, keyvals ...any)
}

// OpenWebIFClient defines the interface required for deleting movies.
type OpenWebIFClient interface {
	DeleteMovie(ctx context.Context, serviceRef string) error
}

// DeleteRecording handles DELETE /recordings/{id}.
// Mandate: Handlers are thin adapters and treat IDs as opaque.
func DeleteRecording(w http.ResponseWriter, r *http.Request, recordingID string, deps DeleteDeps) {
	serviceRef := recordingID // Treated as opaque link to OpenWebIF

	client := deps.NewOWIClient()
	if err := client.DeleteMovie(r.Context(), serviceRef); err != nil {
		// Log with keyvals
		if deps.Logger != nil {
			deps.Logger("failed to delete recording", "ref", serviceRef, "error", err)
		}

		status, typ, title, code, detail := classifyDeleteError(err)
		deps.WriteProblem(w, r, status, typ, title, code, detail)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func classifyDeleteError(err error) (int, string, string, string, string) {
	if errors.Is(err, openwebif.ErrNotFound) {
		return http.StatusNotFound, "recordings/not-found", "Not Found", "NOT_FOUND", "Recording not found"
	}
	if errors.Is(err, openwebif.ErrForbidden) {
		return http.StatusForbidden, "recordings/upstream-auth", "Upstream Auth Failed", "UPSTREAM_AUTH", "Receiver rejected credentials or access forbidden"
	}
	if errors.Is(err, openwebif.ErrTimeout) {
		return http.StatusGatewayTimeout, "recordings/upstream-timeout", "Upstream Timeout", "UPSTREAM_TIMEOUT", "Receiver request timed out"
	}
	if errors.Is(err, openwebif.ErrUpstreamUnavailable) {
		return http.StatusBadGateway, "recordings/upstream-unavailable", "Upstream Unavailable", "UPSTREAM_UNAVAILABLE", "Receiver unreachable"
	}
	if errors.Is(err, openwebif.ErrUpstreamError) {
		return http.StatusBadGateway, "recordings/upstream", "Upstream Error", "UPSTREAM_ERROR", "Receiver error (5xx)"
	}
	if errors.Is(err, openwebif.ErrUpstreamBadResponse) {
		return http.StatusBadGateway, "recordings/upstream", "Upstream Error", "UPSTREAM_ERROR", "Receiver sent invalid response"
	}

	return http.StatusInternalServerError, "recordings/delete_failed", "Delete Failed", "DELETE_FAILED", "An unexpected error occurred"
}
