// SPDX-License-Identifier: MIT
package playlist

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

type Item struct {
	Name    string
	TvgID   string
	TvgChNo int
	TvgLogo string
	Group   string
	URL     string
}

func WriteM3U(w io.Writer, items []Item) error {
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
		fmt.Fprintf(&attrs, `tvg-logo="%s" `, it.TvgLogo)
		fmt.Fprintf(&attrs, `group-title="%s" `, it.Group)
		// tvg-name duplicates channel name to improve EPG mapping in some clients
		fmt.Fprintf(&attrs, `tvg-name="%s"`, it.Name)

		fmt.Fprintf(buf, `#EXTINF:-1 %s,%s`+"\n", attrs.String(), it.Name)
		buf.WriteString(it.URL + "\n")
	}
	_, err := io.Copy(w, buf)
	return err
}
