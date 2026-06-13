// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import (
	"errors"
	"testing"
)

func TestNormalizeWebBootstrapTargetPath_RejectsOpenRedirect(t *testing.T) {
	// Browsers normalize "\" to "/" before navigating, so these all resolve to an
	// external host once the SeeOther redirect is followed.
	reject := []string{
		`/\evil.com`,
		`/\/evil.com`,
		`\/evil.com`,
		`/\\evil.com`,
		`//evil.com`, // protocol-relative
		`https://evil.com`,
		`http://evil.com/path`,
		"/ui/\r\nSet-Cookie: x=1", // header injection
	}
	for _, in := range reject {
		if _, err := normalizeWebBootstrapTargetPath(in); !errors.Is(err, ErrInvalidWebBootstrapTargetPath) {
			t.Errorf("normalizeWebBootstrapTargetPath(%q): expected rejection, got err=%v", in, err)
		}
	}

	accept := map[string]string{
		"/ui/":          "/ui/",
		"/ui/devices":   "/ui/devices",
		"/ui/x?next=/a": "/ui/x?next=/a",
		"":              "/ui/", // empty defaults to /ui/
	}
	for in, want := range accept {
		got, err := normalizeWebBootstrapTargetPath(in)
		if err != nil {
			t.Errorf("normalizeWebBootstrapTargetPath(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeWebBootstrapTargetPath(%q): got %q, want %q", in, got, want)
		}
	}
}
