package resolver

import (
	"context"
	"fmt"
	"net/http"
)

// DefaultProbe performs a standard HTTP HEAD check to verify source availability.
// It bypasses the probe logic if the source is local file (file://).
func DefaultProbe(ctx context.Context, sourceURL string) error {
	// 1. Local Files: Assumed available if resolved
	if len(sourceURL) > 7 && sourceURL[:7] == "file://" {
		return nil
	}

	// 2. HTTP Probing
	req, err := http.NewRequestWithContext(ctx, "HEAD", sourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("probe failed with status %d", resp.StatusCode)
	}
	return nil
}
