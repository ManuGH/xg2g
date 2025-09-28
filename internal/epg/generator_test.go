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
	tv := GenerateXMLTV(channels)
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
	if err := WriteXMLTV(channels, p); err != nil {
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
				Title:   Title{Lang: "en", Value: "Show1"},
				Desc:    "Description1",
			},
			{
				Start:   "202501010100 +0000",
				Stop:    "202501010200 +0000",
				Channel: "c1",
				Title:   Title{Value: "Show2"},
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
	if p0.Title.Value != "Show1" {
		t.Fatalf("expected programme 0 title Show1, got %s", p0.Title.Value)
	}
	if p0.Title.Lang != "en" {
		t.Fatalf("expected programme 0 title lang en, got %s", p0.Title.Lang)
	}

	p1 := got.Programs[1]
	if p1.Title.Value != "Show2" {
		t.Fatalf("expected programme 1 title Show2, got %s", p1.Title.Value)
	}
	if p1.Title.Lang != "" {
		t.Fatalf("expected programme 1 title lang empty, got %s", p1.Title.Lang)
	}
}

func TestTitleOmitEmptyLang(t *testing.T) {
	b, err := xml.Marshal(Title{Value: "Foo"})
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
		GeneratorURL: "https://example.invalid/xg2g",
		Channels:     []Channel{{ID: "c1", DisplayName: []string{"Chan1"}}},
		Programs: []Programme{
			{Start: "202501010000 +0000", Stop: "202501010100 +0000", Channel: "c1", Title: Title{Lang: "en", Value: "Show1"}, Desc: "Description1"},
			{Start: "202501010100 +0000", Stop: "202501010200 +0000", Channel: "c1", Title: Title{Value: "Show2"}},
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
