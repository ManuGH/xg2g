// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package epg_test

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/epg"
)

func TestResolveEPGRating(t *testing.T) {
	tests := []struct {
		name              string
		broadcasterRating string
		title             string
		description       string
		overrides         map[string]int
		itemID            string
		wantRating        int
		wantUnknown       bool
		wantSource        string
	}{
		{
			name:        "Override match",
			overrides:   map[string]int{"event-101": 16},
			itemID:      "event-101",
			wantRating:  16,
			wantUnknown: false,
			wantSource:  "override",
		},
		{
			name:              "Broadcaster rating numeric string",
			broadcasterRating: "12",
			wantRating:        12,
			wantUnknown:       false,
			wantSource:        "broadcaster",
		},
		{
			name:        "Text analysis FSK 16 in description",
			title:       "Spannender Thriller",
			description: "Dieser Film ist FSK 16 freigegeben.",
			wantRating:  16,
			wantUnknown: false,
			wantSource:  "text_analysis",
		},
		{
			name:        "Text analysis ab 18 in title",
			title:       "Horror Movie (ab 18)",
			description: "Nichts für schwache Nerven",
			wantRating:  18,
			wantUnknown: false,
			wantSource:  "text_analysis",
		},
		{
			name:        "Text analysis FSK o.A.",
			title:       "Kinder-Zeichentrick",
			description: "FSK o.A. für die ganze Familie",
			wantRating:  0,
			wantUnknown: false,
			wantSource:  "text_analysis",
		},
		{
			name:        "Fallback UNKNOWN for unrated content",
			title:       "Abendjournal",
			description: "Aktuelle Nachrichten aus Österreich und der Welt",
			wantRating:  epg.RatingUnknown,
			wantUnknown: true,
			wantSource:  "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := epg.ResolveEPGRating(tt.broadcasterRating, tt.title, tt.description, tt.overrides, tt.itemID)
			if res.Rating != tt.wantRating {
				t.Errorf("Rating = %d, want %d", res.Rating, tt.wantRating)
			}
			if res.IsUnknown != tt.wantUnknown {
				t.Errorf("IsUnknown = %v, want %v", res.IsUnknown, tt.wantUnknown)
			}
			if res.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", res.Source, tt.wantSource)
			}
		})
	}
}

func TestIsRatingAllowed(t *testing.T) {
	t.Run("Rating within max limit allowed", func(t *testing.T) {
		allowed, reqAppr := epg.IsRatingAllowed(12, false, 12, epg.UnknownPolicyBlock)
		if !allowed || reqAppr {
			t.Errorf("expected allowed=true, reqAppr=false, got allowed=%v, reqAppr=%v", allowed, reqAppr)
		}
	})

	t.Run("Rating exceeds max limit requires approval", func(t *testing.T) {
		allowed, reqAppr := epg.IsRatingAllowed(16, false, 12, epg.UnknownPolicyBlock)
		if allowed || !reqAppr {
			t.Errorf("expected allowed=false, reqAppr=true, got allowed=%v, reqAppr=%v", allowed, reqAppr)
		}
	})

	t.Run("UNKNOWN policy block", func(t *testing.T) {
		allowed, reqAppr := epg.IsRatingAllowed(epg.RatingUnknown, true, 12, epg.UnknownPolicyBlock)
		if allowed || reqAppr {
			t.Errorf("expected allowed=false, reqAppr=false, got allowed=%v, reqAppr=%v", allowed, reqAppr)
		}
	})

	t.Run("UNKNOWN policy request approval", func(t *testing.T) {
		allowed, reqAppr := epg.IsRatingAllowed(epg.RatingUnknown, true, 12, epg.UnknownPolicyRequestApproval)
		if allowed || !reqAppr {
			t.Errorf("expected allowed=false, reqAppr=true, got allowed=%v, reqAppr=%v", allowed, reqAppr)
		}
	})

	t.Run("UNKNOWN policy allow", func(t *testing.T) {
		allowed, reqAppr := epg.IsRatingAllowed(epg.RatingUnknown, true, 12, epg.UnknownPolicyAllow)
		if !allowed || reqAppr {
			t.Errorf("expected allowed=true, reqAppr=false, got allowed=%v, reqAppr=%v", allowed, reqAppr)
		}
	})
}
