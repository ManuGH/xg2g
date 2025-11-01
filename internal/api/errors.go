// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// writeJSON writes a JSON response with the given status code
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a generic error response
func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

// writeUnauthorized writes a 401 Unauthorized response
func writeUnauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

// writeForbidden writes a 403 Forbidden response
func writeForbidden(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
}

// writeNotFound writes a 404 Not Found response
func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// writeServiceUnavailable writes a 503 Service Unavailable response
func writeServiceUnavailable(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
}

// setDownloadHeaders sets appropriate headers for file downloads
func setDownloadHeaders(w http.ResponseWriter, name string, size int64, mod time.Time) {
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Last-Modified", mod.UTC().Format(http.TimeFormat))

	// Set Content-Type based on file extension
	switch {
	case strings.HasSuffix(name, ".m3u"), strings.HasSuffix(name, ".m3u8"):
		w.Header().Set("Content-Type", "audio/x-mpegurl")
	case strings.HasSuffix(name, ".xml"):
		w.Header().Set("Content-Type", "application/xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
}
