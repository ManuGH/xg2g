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
	var b bytes.Buffer
	b.WriteString("#EXTM3U\n")
	
	for _, it := range items {
		// Attribute nur schreiben, wenn gesetzt
		line := "#EXTINF:-1"
		if it.TvgChNo > 0 {
			line += fmt.Sprintf(` tvg-chno="%d"`, it.TvgChNo)
		}
		if it.TvgID != "" {
			line += fmt.Sprintf(` tvg-id="%s"`, it.TvgID)
		}
		if it.TvgLogo != "" {
			line += fmt.Sprintf(` tvg-logo="%s"`, it.TvgLogo)
		}
		if it.Group != "" {
			line += fmt.Sprintf(` group-title="%s"`, it.Group)
		}
		line += fmt.Sprintf(",%s\n", it.Name)
		b.WriteString(line)
		b.WriteString(it.URL + "\n")
	}
	
	_, err := w.Write(b.Bytes())
	return err
}
