package recordings

import (
	"context"
	"errors"
	"net/http"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/problemcode"
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
		return resolvedDeleteProblem(http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording not found")
	}
	if errors.Is(err, openwebif.ErrForbidden) {
		return resolvedDeleteProblem(http.StatusForbidden, "recordings/upstream-auth", "Upstream Auth Failed", problemcode.CodeUpstreamAuth, "Receiver rejected credentials or access forbidden")
	}
	if errors.Is(err, openwebif.ErrTimeout) {
		return resolvedDeleteProblem(http.StatusGatewayTimeout, "recordings/upstream-timeout", "Upstream Timeout", problemcode.CodeUpstreamTimeout, "Receiver request timed out")
	}
	if errors.Is(err, openwebif.ErrUpstreamUnavailable) {
		return resolvedDeleteProblem(http.StatusBadGateway, "recordings/upstream-unavailable", "Upstream Unavailable", problemcode.CodeUpstreamUnavailable, "Receiver unreachable")
	}
	if errors.Is(err, openwebif.ErrUpstreamError) {
		return resolvedDeleteProblem(http.StatusBadGateway, "recordings/upstream", "Upstream Error", problemcode.CodeUpstreamError, "Receiver error (5xx)")
	}
	if errors.Is(err, openwebif.ErrUpstreamBadResponse) {
		return resolvedDeleteProblem(http.StatusBadGateway, "recordings/upstream", "Upstream Error", problemcode.CodeUpstreamError, "Receiver sent invalid response")
	}

	return resolvedDeleteProblem(http.StatusInternalServerError, "recordings/delete_failed", "Delete Failed", problemcode.CodeDeleteFailed, "An unexpected error occurred")
}

func resolvedDeleteProblem(status int, typ, title, code, detail string) (int, string, string, string, string) {
	spec := problemcode.MustResolve(code, title)
	return status, typ, spec.Title, spec.Code, detail
}
