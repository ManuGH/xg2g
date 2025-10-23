// Package epg provides Electronic Program Guide (EPG) functionality including fuzzy matching and XMLTV generation.

// SPDX-License-Identifier: MIT
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
	// Value is the character data of the title element.
	Value string `xml:",chardata"`
}

// GenerateXMLTV generates an XMLTV document from channel and EPG data.

func GenerateXMLTV(channels []Channel) *TV {
	return &TV{
		Generator:    "xg2g",
		GeneratorURL: "https://example.invalid/xg2g",
		Channels:     channels,
		Programs:     []Programme{},
	}
}

// GenerateXMLTVWithProgrammes creates a TV struct with both channels and programmes
// GenerateXMLTV generates an XMLTV document from channel and EPG data.

func GenerateXMLTVWithProgrammes(channels []Channel, programmes []Programme) *TV {
	return &TV{
		Generator:    "xg2g",
		GeneratorURL: "https://example.invalid/xg2g",
		Channels:     channels,
		Programs:     programmes,
	}
}

// WriteXMLTV writes XMLTV data to a file.

func WriteXMLTV(channels []Channel, path string) error {
	tv := GenerateXMLTV(channels)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "xmltv-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Write XML header with UTF-8 encoding
	if _, err := tmp.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	// Use xml.Encoder to ensure proper UTF-8 encoding
	encoder := xml.NewEncoder(tmp)
	encoder.Indent("", "  ")
	if err := encoder.Encode(tv); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// WriteXMLTVWithProgrammes writes XMLTV with both channels and programmes to path
// WriteXMLTV writes XMLTV data to a file.

func WriteXMLTVWithProgrammes(channels []Channel, programmes []Programme, path string) error {
	tv := GenerateXMLTVWithProgrammes(channels, programmes)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "xmltv-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Write XML header with UTF-8 encoding
	if _, err := tmp.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	// Use xml.Encoder to ensure proper UTF-8 encoding
	encoder := xml.NewEncoder(tmp)
	encoder.Indent("", "  ")
	if err := encoder.Encode(tv); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
