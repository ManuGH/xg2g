package epg

import (
	"encoding/xml"
	"io"
	"os"
	"regexp"
	"strings"
)

type tv struct {
	XMLName  xml.Name  `xml:"tv"`
	Channels []channel `xml:"channel"`
}

type channel struct {
	ID          string   `xml:"id,attr"`
	DisplayName []string `xml:"display-name"`
}

var (
	suffix = regexp.MustCompile(`\s+(hd|uhd|4k|austria|österreich|oesterreich|at|de|ch)$`)
	space  = regexp.MustCompile(`\s+`)
)

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = suffix.ReplaceAllString(s, "")
	s = space.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func BuildNameToIDMap(xmltvPath string) (map[string]string, error) {
	f, err := os.Open(xmltvPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var doc tv
	dec := xml.NewDecoder(f)
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil && err != io.EOF {
		return nil, err
	}

	out := make(map[string]string, len(doc.Channels))
	for _, ch := range doc.Channels {
		if ch.ID == "" || len(ch.DisplayName) == 0 {
			continue
		}
		key := norm(ch.DisplayName[0])
		if key != "" {
			out[key] = ch.ID
		}
	}
	return out, nil
}

func NameKey(s string) string { return norm(s) }
