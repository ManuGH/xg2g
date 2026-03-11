package v3

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

func TestScheduleEntriesFromProgrammesMatchesCanonicalServiceRef(t *testing.T) {
	programmes := []epg.Programme{
		{
			Channel: "1:0:1:AAA",
			Start:   "20260307110000 +0000",
			Stop:    "20260307120000 +0000",
			Title:   epg.Title{Text: "Current Show"},
			Desc:    &epg.Description{Text: "Current Description"},
		},
		{
			Channel: "1:0:1:AAA:",
			Start:   "20260307120000 +0000",
			Stop:    "20260307130000 +0000",
			Title:   epg.Title{Text: "Next Show"},
			Desc:    &epg.Description{Text: "Next Description"},
		},
		{
			Channel: "1:0:1:BBB:",
			Start:   "20260307110000 +0000",
			Stop:    "20260307120000 +0000",
			Title:   epg.Title{Text: "Other Channel"},
		},
	}

	entries := scheduleEntriesFromProgrammes("1:0:1:AAA:", programmes)
	if len(entries) != 2 {
		t.Fatalf("expected 2 schedule entries, got %d", len(entries))
	}
	if entries[0].title != "Current Show" {
		t.Fatalf("expected first title to be Current Show, got %q", entries[0].title)
	}
	if entries[1].title != "Next Show" {
		t.Fatalf("expected second title to be Next Show, got %q", entries[1].title)
	}
}

func TestMergeCurrentInfoFromScheduleFillsMissingNowAndNext(t *testing.T) {
	current := &openwebif.CurrentInfo{}
	now := time.Date(2026, time.March, 7, 11, 30, 0, 0, time.UTC)
	entries := []scheduleEntry{
		{
			title: "Current Show",
			desc:  "Current Description",
			start: time.Date(2026, time.March, 7, 11, 0, 0, 0, time.UTC).Unix(),
			end:   time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC).Unix(),
		},
		{
			title: "Next Show",
			desc:  "Next Description",
			start: time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC).Unix(),
			end:   time.Date(2026, time.March, 7, 13, 0, 0, 0, time.UTC).Unix(),
		},
	}

	changed := mergeCurrentInfoFromSchedule(current, now, entries)
	if !changed {
		t.Fatal("expected merge to report changes")
	}
	if current.Now.EventTitle != "Current Show" {
		t.Fatalf("expected current title to be filled, got %q", current.Now.EventTitle)
	}
	if current.Now.EventDescription != "Current Description" {
		t.Fatalf("expected current description to be filled, got %q", current.Now.EventDescription)
	}
	if current.Now.EventStart != entries[0].start {
		t.Fatalf("expected current start %d, got %d", entries[0].start, current.Now.EventStart)
	}
	if current.Now.EventDuration != 3600 {
		t.Fatalf("expected current duration 3600, got %d", current.Now.EventDuration)
	}
	if current.Next.EventTitle != "Next Show" {
		t.Fatalf("expected next title to be filled, got %q", current.Next.EventTitle)
	}
}

func TestMergeCurrentInfoFromSchedulePreservesExistingFields(t *testing.T) {
	current := &openwebif.CurrentInfo{}
	current.Now.EventTitle = "Receiver Title"
	current.Next.EventTitle = "Receiver Next"
	now := time.Date(2026, time.March, 7, 11, 30, 0, 0, time.UTC)
	entries := []scheduleEntry{
		{
			title: "Fallback Current",
			desc:  "Fallback Description",
			start: time.Date(2026, time.March, 7, 11, 0, 0, 0, time.UTC).Unix(),
			end:   time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC).Unix(),
		},
		{
			title: "Fallback Next",
			desc:  "Fallback Next Description",
			start: time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC).Unix(),
			end:   time.Date(2026, time.March, 7, 13, 0, 0, 0, time.UTC).Unix(),
		},
	}

	mergeCurrentInfoFromSchedule(current, now, entries)
	if current.Now.EventTitle != "Receiver Title" {
		t.Fatalf("expected current title to stay unchanged, got %q", current.Now.EventTitle)
	}
	if current.Next.EventTitle != "Receiver Next" {
		t.Fatalf("expected next title to stay unchanged, got %q", current.Next.EventTitle)
	}
	if current.Now.EventDescription != "Fallback Description" {
		t.Fatalf("expected missing description to be filled, got %q", current.Now.EventDescription)
	}
}
