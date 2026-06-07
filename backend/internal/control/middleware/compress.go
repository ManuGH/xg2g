// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package middleware

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// defaultCompressionLevel mirrors a sensible gzip default: level 5 trades a
// little CPU for a solid ratio, well short of the diminishing returns of 9.
const defaultCompressionLevel = 5

// compressibleContentTypes is the allowlist of response content types that are
// worth gzipping. It is chi's default text-ish set plus the two HLS *playlist*
// (manifest) types.
//
// Media SEGMENTS (video/*, audio/*, application/octet-stream, image/jpeg, ...)
// are intentionally absent: they are already compressed, gzipping them only
// burns CPU, and — critically for the player — keeping them out preserves
// HTTP Range requests, which gzip would break. Anything not on this list is
// streamed through untouched.
var compressibleContentTypes = []string{
	"text/html",
	"text/css",
	"text/plain",
	"text/javascript",
	"application/javascript",
	"application/x-javascript",
	"application/json",
	"application/atom+xml",
	"application/rss+xml",
	"image/svg+xml",
	// HLS manifests are plain text and compress very well.
	"application/vnd.apple.mpegurl",
	"application/x-mpegurl",
}

// Compression returns response-compression middleware (gzip/deflate negotiated
// via Accept-Encoding) scoped to text-ish payloads and HLS playlists. Media
// segments are never compressed — see compressibleContentTypes.
//
// Requests carrying a Range header bypass compression entirely: gzipping a
// partial (206) response would produce an invalid gzip stream the client could
// not decode, corrupting range-based media seeks and chunked asset loading.
func Compression() func(http.Handler) http.Handler {
	compressor := chimw.Compress(defaultCompressionLevel, compressibleContentTypes...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Range") != "" {
				next.ServeHTTP(w, r)
				return
			}
			compressor(next).ServeHTTP(w, r)
		})
	}
}
