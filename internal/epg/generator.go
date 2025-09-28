package epg

import (
	"encoding/xml"
	"os"
	"path/filepath"
)

type TV struct {
	XMLName      xml.Name    `xml:"tv"`
	Generator    string      `xml:"generator-info-name,attr,omitempty"`
	GeneratorURL string      `xml:"generator-info-url,attr,omitempty"`
	Channels     []Channel   `xml:"channel"`
	Programs     []Programme `xml:"programme"`
}

type Channel struct {
	ID          string   `xml:"id,attr"`
	DisplayName []string `xml:"display-name"`
	Icon        *Icon    `xml:"icon,omitempty"`
}

type Icon struct {
	Src string `xml:"src,attr"`
}

type Programme struct {
	Start   string `xml:"start,attr"`
	Stop    string `xml:"stop,attr"`
	Channel string `xml:"channel,attr"`
	Title   Title  `xml:"title"`
	Desc    string `xml:"desc,omitempty"`
}

type Title struct {
	// Lang contains the language code for the title (optional).
	Lang string `xml:"lang,attr,omitempty"`
	// Value is the character data of the title element.
	Value string `xml:",chardata"`
}

func GenerateXMLTV(channels []Channel) *TV {
	return &TV{
		Generator:    "xg2g",
		GeneratorURL: "https://example.invalid/xg2g",
		Channels:     channels,
		Programs:     []Programme{},
	}
}

func WriteXMLTV(channels []Channel, path string) error {
	tv := GenerateXMLTV(channels)
	out, err := xml.MarshalIndent(tv, "", "  ")
	if err != nil {
		return err
	}

	xmlHeader := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	completeXML := xmlHeader + string(out)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "xmltv-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(completeXML); err != nil {
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
