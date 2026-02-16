// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package system

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

type setupValidateRequest struct {
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupValidateResponse struct {
	Valid    bool                 `json:"valid"`
	Message  string               `json:"message"`
	Bouquets []string             `json:"bouquets,omitempty"`
	Version  *openwebif.AboutInfo `json:"version,omitempty"`
}

// NewSetupValidateHandler validates OpenWebIF connectivity and metadata.
func NewSetupValidateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req setupValidateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.BaseURL == "" {
			http.Error(w, "Base URL is required", http.StatusBadRequest)
			return
		}

		// Normalize lazy user input to keep setup UX permissive.
		if !strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://") {
			req.BaseURL = "http://" + req.BaseURL
		}

		client := openwebif.NewWithPort(req.BaseURL, 0, openwebif.Options{
			Timeout:  5 * time.Second,
			Username: req.Username,
			Password: req.Password,
		})

		about, err := client.About(r.Context())
		if err != nil {
			log.L().Warn().Err(err).Str("baseUrl", req.BaseURL).Msg("validation failed: connection error")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(setupValidateResponse{
				Valid:   false,
				Message: fmt.Sprintf("Connection failed: %s", err.Error()),
			})
			return
		}

		bouquetsMap, err := client.Bouquets(r.Context())
		if err != nil {
			log.L().Warn().Err(err).Msg("validation warning: could not fetch bouquets")
		}

		bouquetsList := make([]string, 0, len(bouquetsMap))
		for name := range bouquetsMap {
			bouquetsList = append(bouquetsList, name)
		}
		sort.Strings(bouquetsList)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(setupValidateResponse{
			Valid:    true,
			Message:  "Connection successful",
			Version:  about,
			Bouquets: bouquetsList,
		})
	}
}
