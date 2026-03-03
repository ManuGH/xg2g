package recordings

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

var (
	ErrDurationInvalid  = errors.New("duration invalid")
	ErrDurationNegative = errors.New("duration negative")
	ErrDurationOverflow = errors.New("duration overflow")
)

// ParseRecordingDurationSeconds reads strings like "01:30:00", "45:00", "90 min" and returns seconds.
func ParseRecordingDurationSeconds(length string) (int64, error) {
	length = strings.TrimSpace(length)
	if length == "" || length == "0" {
		return 0, ErrDurationInvalid
	}

	// Case 1: HH:MM:SS or MM:SS
	if strings.Contains(length, ":") {
		parts := strings.Split(length, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return 0, ErrDurationInvalid
		}

		cleanParts := make([]int64, len(parts))
		for i := range parts {
			s := strings.TrimSpace(parts[i])
			if s == "" {
				return 0, ErrDurationInvalid
			}
			val, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return 0, ErrDurationInvalid
			}
			if val < 0 {
				return 0, ErrDurationNegative
			}
			cleanParts[i] = val
		}

		if len(parts) == 3 {
			if cleanParts[1] >= 60 || cleanParts[2] >= 60 {
				return 0, ErrDurationInvalid
			}
		} else { // len(parts) == 2
			if cleanParts[1] >= 60 {
				return 0, ErrDurationInvalid
			}
		}

		var total int64
		if len(parts) == 3 {
			// HH:MM:SS
			if cleanParts[0] > math.MaxInt64/3600 {
				return 0, ErrDurationOverflow
			}
			total = cleanParts[0] * 3600

			term2 := cleanParts[1] * 60
			if total > math.MaxInt64-term2 {
				return 0, ErrDurationOverflow
			}
			total += term2

			if total > math.MaxInt64-cleanParts[2] {
				return 0, ErrDurationOverflow
			}
			total += cleanParts[2]
		} else {
			// MM:SS
			if cleanParts[0] > math.MaxInt64/60 {
				return 0, ErrDurationOverflow
			}
			total = cleanParts[0] * 60

			if total > math.MaxInt64-cleanParts[1] {
				return 0, ErrDurationOverflow
			}
			total += cleanParts[1]
		}

		if total <= 0 {
			return 0, ErrDurationInvalid
		}
		return total, nil
	}

	// Case 2: Numeric with suffix (e.g. "90 min")
	fields := strings.Fields(length)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, ErrDurationInvalid
	}

	suffixes := []string{"minutes", "mins", "min", "min.", "m"}
	minStr := fields[0]

	if len(fields) == 2 {
		found := false
		suffix := strings.ToLower(fields[1])
		for _, s := range suffixes {
			if suffix == s {
				found = true
				break
			}
		}
		if !found {
			return 0, ErrDurationInvalid
		}
	} else {
		foundSuffix := ""
		lowerStr := strings.ToLower(minStr)
		for _, s := range suffixes {
			if strings.HasSuffix(lowerStr, s) {
				foundSuffix = s
				break
			}
		}
		if foundSuffix != "" {
			minStr = minStr[:len(minStr)-len(foundSuffix)]
		}
	}

	minutes, err := strconv.ParseInt(minStr, 10, 64)
	if err != nil {
		return 0, ErrDurationInvalid
	}
	if minutes < 0 {
		return 0, ErrDurationNegative
	}
	if minutes > math.MaxInt64/60 {
		return 0, ErrDurationOverflow
	}
	total := minutes * 60
	if total <= 0 {
		return 0, ErrDurationInvalid
	}
	return total, nil
}
