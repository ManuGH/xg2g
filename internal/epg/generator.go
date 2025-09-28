package epg

import (
	"encoding/xml"
	"os"
)

type TV struct {
	XMLName   xml.Name    `xml:"tv"`
	Generator string      `xml:"generator-info-name,attr,omitempty"`
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
	// Lang contains the language code for the title (optional).
	Lang string `xml:"lang,attr,omitempty"`
	// Value is the character data of the title element.
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

	xmlHeader := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	completeXML := xmlHeader + string(out)

	return os.WriteFile(path, []byte(completeXML), 0644)
}
