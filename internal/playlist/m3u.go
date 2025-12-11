// SPDX-License-Identifier: MIT

// Package playlist provides M3U playlist generation and manipulation.
package playlist

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// Item represents an M3U playlist entry with channel metadata.
type Item struct {
	Name    string
	TvgID   string
	TvgChNo int
	TvgLogo string
	Group   string
	URL     string
}

// WriteM3U writes an M3U playlist to the given writer.
func WriteM3U(w io.Writer, items []Item, publicURL string) error {
	buf := &bytes.Buffer{}
	// Optional x-tvg-url header attribute for clients that auto-load EPG
	if epgURL := os.Getenv("XG2G_X_TVG_URL"); epgURL != "" {
		// Some players support x-tvg-url (unofficial but widely used)
		fmt.Fprintf(buf, `#EXTM3U x-tvg-url="%s"`+"\n", epgURL)
	} else {
		buf.WriteString("#EXTM3U\n")
	}
	for _, it := range items {
		// Build attributes dynamically to omit tvg-chno when unset and include tvg-name
		attrs := bytes.Buffer{}
		// tvg-chno only when > 0
		if it.TvgChNo > 0 {
			fmt.Fprintf(&attrs, `tvg-chno="%d" `, it.TvgChNo)
		}
		fmt.Fprintf(&attrs, `tvg-id="%s" `, it.TvgID)

		// Handle Logo URL (prepend publicURL if set and logo is relative)
		logo := it.TvgLogo
		if publicURL != "" && strings.HasPrefix(logo, "/") {
			// Ensure no double slash if publicURL has trailing slash (it shouldn't, but safe)
			base := strings.TrimRight(publicURL, "/")
			logo = base + logo
		}
		fmt.Fprintf(&attrs, `tvg-logo="%s" `, logo)

		fmt.Fprintf(&attrs, `group-title="%s" `, it.Group)
		// tvg-name duplicates channel name to improve EPG mapping in some clients
		fmt.Fprintf(&attrs, `tvg-name="%s"`, it.Name)

		fmt.Fprintf(buf, `#EXTINF:-1 %s,%s`+"\n", attrs.String(), it.Name)
		buf.WriteString(it.URL + "\n")
	}
	_, err := io.Copy(w, buf)
	return err
}
