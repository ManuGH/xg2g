// SPDX-License-Identifier: MIT
package playlist

import (
	"bytes"
	"fmt"
	"io"
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
	buf.WriteString("#EXTM3U\n")
	for _, it := range items {
		buf.WriteString(fmt.Sprintf(
			`#EXTINF:-1 tvg-chno="%d" tvg-id="%s" tvg-logo="%s" group-title="%s",%s`+"\n",
			it.TvgChNo, it.TvgID, it.TvgLogo, it.Group, it.Name,
		))
		buf.WriteString(it.URL + "\n")
	}
	_, err := io.Copy(w, buf)
	return err
}
