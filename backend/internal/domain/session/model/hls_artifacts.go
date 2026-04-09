// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import (
	"path/filepath"

	platformpaths "github.com/ManuGH/xg2g/internal/platform/paths"
)

const SessionFirstFrameMarkerFilename = ".first_frame"

func SessionHLSDir(hlsRoot, sessionID string) string {
	return platformpaths.LiveSessionDir(hlsRoot, sessionID)
}

func SessionFirstFrameMarkerPath(hlsRoot, sessionID string) string {
	dir := SessionHLSDir(hlsRoot, sessionID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, SessionFirstFrameMarkerFilename)
}
