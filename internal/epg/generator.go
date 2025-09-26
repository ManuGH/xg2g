package epg

import (
	"encoding/xml"
	"os"
)

type TV struct {
	XMLName   xml.Name    `xml:"tv"`
	Generator string      `xml:"generator-info-name,attr"`
	Channels  []Channel   `xml:"channel"`
	Programs  []Programme `xml:"programme"`
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
	Lang  string `xml:"lang,attr"`
	Value string `xml:",chardata"`
}

func GenerateXMLTV(channels []Channel) *TV {
	return &TV{
		Generator: "xg2g",
		Channels:  channels,
		Programs:  []Programme{},
	}
}

func WriteXMLTV(channels []Channel, path string) error {
	tv := GenerateXMLTV(channels)
	out, err := xml.MarshalIndent(tv, "", "  ")
	if err != nil {
		return err
	}
	h := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	return os.WriteFile(path, append(h, out...), 0644)
}
