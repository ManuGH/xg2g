// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package epg

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGenerateXMLTV(t *testing.T) {
	channels := []Channel{{ID: "c1", DisplayName: []string{"Test Channel"}}}
	tv := GenerateXMLTV(channels, nil)
	if tv.Generator != "xg2g" {
		t.Fatalf("expected generator xg2g, got %s", tv.Generator)
	}
	if len(tv.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(tv.Channels))
	}
}

func TestWriteXMLTV(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.xml")
	channels := []Channel{{ID: "c1", DisplayName: []string{"C1"}}}
	if err := WriteXMLTV(GenerateXMLTV(channels, nil), p); err != nil {
		t.Fatalf("WriteXMLTV failed: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected file to exist, stat error: %v", err)
	}
}

func TestXMLStructureValidation(t *testing.T) {
	// Build a TV object with one channel and two programmes (one with Lang, one without)
	tv := TV{
		Generator: "xg2g",
		Channels: []Channel{
			{ID: "c1", DisplayName: []string{"Chan1"}},
		},
		Programs: []Programme{
			{
				Start:   "202501010000 +0000",
				Stop:    "202501010100 +0000",
				Channel: "c1",
				Title:   Title{Lang: "en", Text: "Show1"},
				Desc:    "Description1",
			},
			{
				Start:   "202501010100 +0000",
				Stop:    "202501010200 +0000",
				Channel: "c1",
				Title:   Title{Text: "Show2"},
			},
		},
	}

	out, err := xml.MarshalIndent(tv, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got TV
	if err := xml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Check channels
	if len(got.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(got.Channels))
	}
	if got.Channels[0].ID != "c1" {
		t.Fatalf("expected channel ID c1, got %s", got.Channels[0].ID)
	}

	// Check programmes
	if len(got.Programs) != 2 {
		t.Fatalf("expected 2 programmes, got %d", len(got.Programs))
	}

	p0 := got.Programs[0]
	if p0.Channel != "c1" {
		t.Fatalf("expected programme 0 channel c1, got %s", p0.Channel)
	}
	if p0.Title.Text != "Show1" {
		t.Fatalf("expected programme 0 title Show1, got %s", p0.Title.Text)
	}
	if p0.Title.Lang != "en" {
		t.Fatalf("expected programme 0 title lang en, got %s", p0.Title.Lang)
	}

	p1 := got.Programs[1]
	if p1.Title.Text != "Show2" {
		t.Fatalf("expected programme 1 title Show2, got %s", p1.Title.Text)
	}
	if p1.Title.Lang != "" {
		t.Fatalf("expected programme 1 title lang empty, got %s", p1.Title.Lang)
	}
}

func TestTitleOmitEmptyLang(t *testing.T) {
	b, err := xml.Marshal(Title{Text: "Foo"})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(b), `lang=`) {
		t.Fatalf("lang darf bei leer nicht erscheinen")
	}
}

func TestGoldenXMLTV(t *testing.T) {
	// Build a TV object that matches the golden file
	tv := TV{
		Generator:    "xg2g",
		GeneratorURL: "https://github.com/ManuGH/xg2g",
		Channels:     []Channel{{ID: "c1", DisplayName: []string{"Chan1"}}},
		Programs: []Programme{
			{Start: "202501010000 +0000", Stop: "202501010100 +0000", Channel: "c1", Title: Title{Lang: "en", Text: "Show1"}, Desc: "Description1"},
			{Start: "202501010100 +0000", Stop: "202501010200 +0000", Channel: "c1", Title: Title{Text: "Show2"}},
		},
	}

	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b, err := xml.MarshalIndent(tv, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	buf.Write(b)
	// Ensure final newline to match golden file
	buf.WriteString("\n")

	want, err := os.ReadFile("testdata/xmltv.golden.xml")
	if err != nil {
		t.Fatalf("read golden failed: %v", err)
	}
	if diff := cmp.Diff(string(want), buf.String()); diff != "" {
		t.Fatalf("(-want +got):\n%s", diff)
	}
}

func TestUmlautsAndUTF8Encoding(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "umlauts.xml")

	channels := []Channel{
		{ID: "orf1", DisplayName: []string{"ÖRF 1"}},
		{ID: "ard", DisplayName: []string{"Das Erste (ARD)"}},
	}

	programmes := []Programme{
		{
			Start:   "202501010000 +0000",
			Stop:    "202501010100 +0000",
			Channel: "orf1",
			Title:   Title{Text: "Tagesschau"},
			Desc:    "Aktuelle Nachrichten aus Österreich",
		},
		{
			Start:   "202501010100 +0000",
			Stop:    "202501010200 +0000",
			Channel: "ard",
			Title:   Title{Text: "Fußball-Bundesliga"},
			Desc:    "München spielt gegen Köln",
		},
	}

	// Write XMLTV with umlauts
	if err := WriteXMLTV(GenerateXMLTV(channels, programmes), p); err != nil {
		t.Fatalf("WriteXMLTV failed: %v", err)
	}

	// Read back and verify UTF-8 encoding
	// #nosec G304 -- p originates from t.TempDir and is controlled by the test
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	xmlStr := string(content)

	// Check for UTF-8 declaration
	if !strings.Contains(xmlStr, `encoding="UTF-8"`) {
		t.Errorf("expected UTF-8 encoding declaration")
	}

	// Verify umlauts are present (not escaped as HTML entities)
	if !strings.Contains(xmlStr, "ÖRF 1") {
		t.Errorf("expected ÖRF 1 to be preserved as UTF-8, got: %s", xmlStr)
	}

	if !strings.Contains(xmlStr, "Österreich") {
		t.Errorf("expected Österreich to be preserved as UTF-8, got: %s", xmlStr)
	}

	if !strings.Contains(xmlStr, "Fußball") {
		t.Errorf("expected Fußball to be preserved as UTF-8, got: %s", xmlStr)
	}

	if !strings.Contains(xmlStr, "München") {
		t.Errorf("expected München to be preserved as UTF-8, got: %s", xmlStr)
	}

	if !strings.Contains(xmlStr, "Köln") {
		t.Errorf("expected Köln to be preserved as UTF-8, got: %s", xmlStr)
	}

	// Parse back to verify validity
	var tv TV
	if err := xml.Unmarshal(content, &tv); err != nil {
		t.Fatalf("failed to parse generated XML: %v", err)
	}

	// Verify channels
	if len(tv.Channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(tv.Channels))
	}

	if tv.Channels[0].DisplayName[0] != "ÖRF 1" {
		t.Errorf("expected 'ÖRF 1', got %q", tv.Channels[0].DisplayName[0])
	}

	// Verify programmes
	if len(tv.Programs) != 2 {
		t.Errorf("expected 2 programmes, got %d", len(tv.Programs))
	}

	if tv.Programs[0].Desc != "Aktuelle Nachrichten aus Österreich" {
		t.Errorf("expected umlaut in description, got %q", tv.Programs[0].Desc)
	}

	if tv.Programs[1].Title.Text != "Fußball-Bundesliga" {
		t.Errorf("expected ß in title, got %q", tv.Programs[1].Title.Text)
	}
}
