package resume

import (
	"context"
	"testing"
	"time"
)

func TestStoreContract_ListRecentOrdersFiltersAndLimits(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openResumeStore(t, backend)
			ctx := context.Background()
			base := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)

			put := func(key string, pos int64, finished bool, at time.Time, title string) {
				t.Helper()
				if err := store.Put(ctx, "viewer", key, &State{
					PosSeconds:      pos,
					DurationSeconds: 3600,
					Finished:        finished,
					UpdatedAt:       at,
					Title:           title,
					Channel:         "ORF1",
				}); err != nil {
					t.Fatalf("put %s: %v", key, err)
				}
			}

			put("rec-old", 100, false, base.Add(-2*time.Hour), "Old")
			put("rec-new", 200, false, base, "New")
			put("rec-mid", 300, false, base.Add(-1*time.Hour), "Mid")
			put("rec-finished", 400, true, base.Add(time.Hour), "Done")
			put("rec-unstarted", 0, false, base.Add(time.Hour), "Unstarted")
			// A different principal must never leak into the listing.
			if err := store.Put(ctx, "other", "rec-foreign", &State{
				PosSeconds: 50, UpdatedAt: base.Add(time.Hour),
			}); err != nil {
				t.Fatalf("put foreign: %v", err)
			}

			entries, err := store.ListRecent(ctx, "viewer", 10)
			if err != nil {
				t.Fatalf("list recent: %v", err)
			}
			wantOrder := []string{"rec-new", "rec-mid", "rec-old"}
			if len(entries) != len(wantOrder) {
				t.Fatalf("expected %d entries, got %d: %+v", len(wantOrder), len(entries), entries)
			}
			for i, want := range wantOrder {
				if entries[i].RecordingKey != want {
					t.Errorf("entry %d: got key %q, want %q", i, entries[i].RecordingKey, want)
				}
			}
			if entries[0].State.Title != "New" || entries[0].State.Channel != "ORF1" {
				t.Errorf("display snapshot lost: %+v", entries[0].State)
			}

			limited, err := store.ListRecent(ctx, "viewer", 2)
			if err != nil {
				t.Fatalf("list recent limited: %v", err)
			}
			if len(limited) != 2 || limited[0].RecordingKey != "rec-new" || limited[1].RecordingKey != "rec-mid" {
				t.Fatalf("limit not applied in recency order: %+v", limited)
			}

			none, err := store.ListRecent(ctx, "viewer", 0)
			if err != nil {
				t.Fatalf("list recent zero: %v", err)
			}
			if len(none) != 0 {
				t.Fatalf("limit 0 must return no entries, got %+v", none)
			}
		})
	}
}

func TestStoreContract_TitleChannelRoundTrip(t *testing.T) {
	backends := []string{"memory", "sqlite"}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			store := openResumeStore(t, backend)
			ctx := context.Background()

			in := &State{
				PosSeconds: 42,
				UpdatedAt:  time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC),
				Title:      "Tatort: Höllenfahrt",
				Channel:    "Das Erste HD",
			}
			if err := store.Put(ctx, "viewer", "rec-1", in); err != nil {
				t.Fatalf("put: %v", err)
			}
			got, err := store.Get(ctx, "viewer", "rec-1")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Title != in.Title || got.Channel != in.Channel {
				t.Fatalf("title/channel roundtrip failed: %+v", got)
			}
		})
	}
}
