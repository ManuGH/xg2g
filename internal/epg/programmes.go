// SPDX-License-Identifier: MIT
package epg

import (
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
)

// ProgrammesFromEPG converts OpenWebIF EPG events to XMLTV Programme format
func ProgrammesFromEPG(events []openwebif.EPGEvent, channelID string) []Programme {
	programmes := make([]Programme, 0, len(events))

	for _, event := range events {
		if event.Title == "" || event.Begin == 0 {
			continue // Skip invalid events
		}

		startTime := time.Unix(event.Begin, 0)
		var stopTime time.Time

		if event.Duration > 0 {
			stopTime = startTime.Add(time.Duration(event.Duration) * time.Second)
		} else {
			// Default 30min duration if not specified
			stopTime = startTime.Add(30 * time.Minute)
		}

		prog := Programme{
			Start:   formatXMLTVTime(startTime),
			Stop:    formatXMLTVTime(stopTime),
			Channel: channelID,
			Title: Title{
				Lang:  "", // No lang attribute for xg2g
				Value: event.Title,
			},
			Desc: buildDescription(event),
		}

		programmes = append(programmes, prog)
	}

	return programmes
}

// formatXMLTVTime formats time in XMLTV format: YYYYMMDDHHMMSS +ZZZZ
func formatXMLTVTime(t time.Time) string {
	return t.Format("20060102150405 -0700")
}

// buildDescription combines short and long descriptions with fallback
func buildDescription(event openwebif.EPGEvent) string {
	if event.LongDesc != "" && event.LongDesc != event.Description {
		return event.LongDesc
	}
	if event.Description != "" {
		return event.Description
	}
	return "" // Empty description is valid
}
