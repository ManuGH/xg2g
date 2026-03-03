// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import "regexp"

var sessionIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// IsSafeSessionID returns true if the ID is safe for filesystem paths and URLs.
func IsSafeSessionID(id string) bool {
	return sessionIDRe.MatchString(id)
}
