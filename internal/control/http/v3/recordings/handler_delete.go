package recordings

import (
	"context"
	"net/http"
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

		// Map errors
		// This requires heuristic mapping or typed errors from OpenWebIF client.
		// For now, simple heuristics based on error string or assumption.
		// Ideally OpenWebIF client returns typed errors.
		status := http.StatusInternalServerError
		code := "DELETE_FAILED"

		// TODO: Import openwebif errors if possible or use string matching
		// if errors.Is(err, openwebif.ErrNotFound) { status = http.StatusNotFound ... }

		deps.WriteProblem(w, r, status, "recordings/delete_failed", "Delete Failed", code, "Failed to delete recording")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
