// Package recordings provides the HTTP handlers and logic for the VOD Recordings API.
// It implements the sub-handlers for Listing, Playback, and HLS streaming.
//
// Layering:
// This package is part of the HTTP Control Layer.
// It depends on:
// - internal/control/vod (Manager/Resolver)
// - internal/control/playback (Engine)
// - internal/infra (FS/Validation)
//
// It MUST NOT depend on:
// - internal/control/http/v3 (Parent Package - except via Interfaces)
package recordings
