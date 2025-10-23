// SPDX-License-Identifier: MIT

// Package epg provides Electronic Program Guide (EPG) functionality including fuzzy matching and XMLTV generation.
package epg

import (
	"encoding/xml"
	"os"
	"path/filepath"
)

// TV represents the root XMLTV document structure.
type TV struct {
	XMLName      xml.Name    `xml:"tv"`
	Generator    string      `xml:"generator-info-name,attr,omitempty"`
	GeneratorURL string      `xml:"generator-info-url,attr,omitempty"`
	Channels     []Channel   `xml:"channel"`
	Programs     []Programme `xml:"programme"`
}

// Channel represents an XMLTV channel with its metadata.
type Channel struct {
	ID          string   `xml:"id,attr"`
	DisplayName []string `xml:"display-name"`
	Icon        *Icon    `xml:"icon,omitempty"`
}

// Icon represents a channel icon in XMLTV format.
type Icon struct {
	Src string `xml:"src,attr"`
}

// Programme represents a TV programme in XMLTV format.
type Programme struct {
	Start   string `xml:"start,attr"`
	Stop    string `xml:"stop,attr"`
	Channel string `xml:"channel,attr"`
	Title   Title  `xml:"title"`
	Desc    string `xml:"desc,omitempty"`
}

// Title represents a programme title with language support.
type Title struct {
	// Lang contains the language code for the title (optional).
	Lang string `xml:"lang,attr,omitempty"`
	// Text is the title text itself.
	Text string `xml:",chardata"`
}

// GenerateXMLTV generates an XMLTV document from channel and EPG data.
func GenerateXMLTV(channels []Channel, programs []Programme) TV {
	return TV{
		Generator:    "xg2g",
		GeneratorURL: "https://github.com/ManuGH/xg2g",
		Channels:     channels,
		Programs:     programs,
	}
}

// WriteXMLTV writes XMLTV data to a file.
func WriteXMLTV(tv TV, outputPath string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	// #nosec G304 -- outputPath is controlled by the application configuration
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, _ = f.WriteString(xml.Header)
	_, _ = f.WriteString(`<!DOCTYPE tv SYSTEM "xmltv.dtd">` + "\n")

	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")
	return enc.Encode(tv)
}
