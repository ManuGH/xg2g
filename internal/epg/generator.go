// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

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
	Start    string   `xml:"start,attr"`
	Stop     string   `xml:"stop,attr"`
	Channel  string   `xml:"channel,attr"`
	Title    Title    `xml:"title"`
	Desc     string   `xml:"desc,omitempty"`
	Credits  *Credits `xml:"credits,omitempty"`
	Date     string   `xml:"date,omitempty"`
	Category []string `xml:"category,omitempty"`
	Country  string   `xml:"country,omitempty"`
}

type Credits struct {
	Director []string `xml:"director,omitempty"`
	Actor    []string `xml:"actor,omitempty"`
	Writer   []string `xml:"writer,omitempty"`
	Producer []string `xml:"producer,omitempty"`
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

// WriteXMLTV writes XMLTV data to a file atomically using temp file + rename.
func WriteXMLTV(tv TV, outputPath string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	// Create temporary file in same directory for atomic rename
	tmpFile, err := os.CreateTemp(dir, "xmltv-*.xml.tmp")
	if err != nil {
		return err
	}

	// Cleanup: close and remove temp file on error
	closed := false
	defer func() {
		if !closed {
			_ = tmpFile.Close()
		}
		// Only remove if rename failed (file still exists)
		if _, statErr := os.Stat(tmpFile.Name()); !os.IsNotExist(statErr) {
			_ = os.Remove(tmpFile.Name())
		}
	}()

	// Write XML content to temporary file
	if _, err := tmpFile.WriteString(xml.Header); err != nil {
		return err
	}
	if _, err := tmpFile.WriteString(`<!DOCTYPE tv SYSTEM "xmltv.dtd">` + "\n"); err != nil {
		return err
	}

	enc := xml.NewEncoder(tmpFile)
	enc.Indent("", "  ")
	if err := enc.Encode(tv); err != nil {
		return err
	}

	// Explicitly close before rename
	if err := tmpFile.Close(); err != nil {
		return err
	}
	closed = true

	// Atomically rename to final destination
	// #nosec G304 -- outputPath is controlled by the application configuration
	return os.Rename(tmpFile.Name(), outputPath)
}
