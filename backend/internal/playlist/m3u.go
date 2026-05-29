// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package playlist provides M3U playlist generation and manipulation.
package playlist

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// Item represents an M3U playlist entry with channel metadata.
type Item struct {
	Name       string
	TvgID      string
	TvgChNo    int
	TvgLogo    string
	Group      string
	URL        string
	ServiceRef string // Internal use: store original sRef for EPG fetching
}

// sanitizeM3UField neutralises values that flow from the upstream box
// (channel/bouquet names, logos) into a single-line #EXTINF record. M3U has no
// attribute-escaping standard, so a raw double quote would break out of a
// quoted attribute and a CR/LF would split the line and inject a fake URL or
// entry. Strip CR/LF and other control characters, replace the quote, and trim.
func sanitizeM3UField(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r == '"':
			b.WriteByte('\'')
		case r < 0x20 || r == 0x7f:
			// drop remaining control characters
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// WriteM3U writes an M3U playlist to the given writer.
func WriteM3U(w io.Writer, items []Item, publicURL string, xTvgURL string) error {
	buf := &bytes.Buffer{}
	// Optional x-tvg-url header attribute for clients that auto-load EPG
	if epgURL := strings.TrimSpace(xTvgURL); epgURL != "" {
		// Some players support x-tvg-url (unofficial but widely used)
		fmt.Fprintf(buf, `#EXTM3U x-tvg-url="%s"`+"\n", sanitizeM3UField(epgURL))
	} else {
		buf.WriteString("#EXTM3U\n")
	}
	for _, it := range items {
		// Upstream channel/bouquet metadata is untrusted; sanitise before it is
		// interpolated into the quoted attributes or the display name.
		name := sanitizeM3UField(it.Name)
		group := sanitizeM3UField(it.Group)
		tvgID := sanitizeM3UField(it.TvgID)

		// Build attributes dynamically to omit tvg-chno when unset and include tvg-name
		attrs := bytes.Buffer{}
		// tvg-chno only when > 0
		if it.TvgChNo > 0 {
			fmt.Fprintf(&attrs, `tvg-chno="%d" `, it.TvgChNo)
		}
		fmt.Fprintf(&attrs, `tvg-id="%s" `, tvgID)

		// Handle Logo URL (prepend publicURL if set and logo is relative)
		logo := it.TvgLogo
		if publicURL != "" && strings.HasPrefix(logo, "/") {
			// Ensure no double slash if publicURL has trailing slash (it shouldn't, but safe)
			base := strings.TrimRight(publicURL, "/")
			logo = base + logo
		}
		fmt.Fprintf(&attrs, `tvg-logo="%s" `, sanitizeM3UField(logo))

		fmt.Fprintf(&attrs, `group-title="%s" `, group)
		// tvg-name duplicates channel name to improve EPG mapping in some clients
		fmt.Fprintf(&attrs, `tvg-name="%s"`, name)

		fmt.Fprintf(buf, `#EXTINF:-1 %s,%s`+"\n", attrs.String(), name)
		buf.WriteString(sanitizeM3UField(it.URL) + "\n")
	}
	_, err := io.Copy(w, buf)
	return err
}
