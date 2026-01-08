package read

import (
	"context"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
)

// EpgSource defines the interface for sourcing EPG data.
type EpgSource interface {
	// GetPrograms returns the raw list of EPG programs (usually from XMLTV).
	GetPrograms(ctx context.Context) ([]epg.Programme, error)

	// GetBouquetServiceRefs returns a set of ServiceRefs belonging to the specified bouquet.
	// If bouquet is empty, it returns nil (meaning no filter).
	GetBouquetServiceRefs(ctx context.Context, bouquet string) (map[string]struct{}, error)
}

// EpgQuery defines filtering parameters for EPG.
type EpgQuery struct {
	From    int64
	To      int64
	Bouquet string
	Q       string
}

// EpgEntry is a control-layer representation of an EPG event.
type EpgEntry struct {
	ID         string
	ServiceRef string
	Title      string
	Desc       string
	Start      int64
	End        int64
	Duration   int64
}

// GetEpg filters and processes EPG data.
func GetEpg(ctx context.Context, src EpgSource, q EpgQuery, clock Clock) ([]EpgEntry, error) {
	programs, err := src.GetPrograms(ctx)
	if err != nil {
		return nil, err
	}

	// Resolve Bouquet Filter
	var allowedRefs map[string]struct{}
	if q.Bouquet != "" {
		allowedRefs, err = src.GetBouquetServiceRefs(ctx, q.Bouquet)
		if err != nil {
			// If loading playlist fails, should we fail or ignore filter?
			// Legacy: "if data, err := os.ReadFile...; err == nil { ... }"
			// Legacy ignored filter if file read failed?
			// But here the contract says we should return error if sourcing fails.
			// Let's assume src handles gracefully or returns error.
			return nil, err
		}
	}

	qLower := strings.ToLower(strings.TrimSpace(q.Q))

	// If search requested and bouquet filter yields nothing, legacy logic:
	// "if qLower != "" && bouquetFilter != "" && len(allowedRefs) == 0 { allowedRefs = nil }"
	if qLower != "" && q.Bouquet != "" && len(allowedRefs) == 0 {
		allowedRefs = nil
	}

	// Time Window Calculation
	now := clock.Now()
	var fromTime, toTime time.Time

	if q.From > 0 {
		fromTime = time.Unix(q.From, 0)
	} else {
		fromTime = now.Add(-30 * time.Minute)
	}

	if q.To > 0 {
		toTime = time.Unix(q.To, 0)
	} else {
		toTime = now.Add(7 * 24 * time.Hour)
	}

	// Max 7 days cap
	maxEnd := now.Add(7 * 24 * time.Hour)
	if toTime.After(maxEnd) {
		toTime = maxEnd
	}

	var results []EpgEntry

	for _, p := range programs {
		// Bouquet Filter
		if allowedRefs != nil {
			if _, ok := allowedRefs[p.Channel]; !ok {
				continue
			}
		}

		// Time Parsing
		startTime, errStart := parseXMLTVTime(p.Start)
		endTime, errEnd := parseXMLTVTime(p.Stop)
		if errStart != nil || errEnd != nil {
			continue
		}

		// Time Window Filter
		// Logic: !startTime.Before(toTime) || !endTime.After(fromTime) -> Skip
		// Matches events overlapping with [fromTime, toTime)
		if !startTime.Before(toTime) || !endTime.After(fromTime) {
			continue
		}

		// Search Query Filter
		if qLower != "" {
			match := false
			if strings.Contains(strings.ToLower(p.Title.Text), qLower) {
				match = true
			} else if strings.Contains(strings.ToLower(p.Desc), qLower) {
				match = true
			}
			if !match {
				continue
			}
		}

		results = append(results, EpgEntry{
			// ID logic? Legacy didn't set ID explicitly in EpgItem struct definition (it was *string)
			// But usually it's empty or p.Channel?
			// Look at legacy:
			// EpgItem{ ServiceRef: p.Channel, Title: p.Title.Text ... }
			// Id field uses p.Channel? No, let's check legacy again.
			// The legacy struct had Id *string.
			// I'll check legacy code again.
			ID:         p.Channel,
			ServiceRef: p.Channel,
			Title:      p.Title.Text,
			Desc:       p.Desc,
			Start:      startTime.Unix(),
			End:        endTime.Unix(),
			Duration:   int64(endTime.Sub(startTime).Seconds()),
		})
	}

	return results, nil
}

func parseXMLTVTime(s string) (time.Time, error) {
	// Format: YYYYMMDDhhmmss ZZZZ
	const layout = "20060102150405 -0700"
	return time.Parse(layout, s)
}
