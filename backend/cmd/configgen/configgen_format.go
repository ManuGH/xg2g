package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func formatDefault(def any) string {
	switch v := def.(type) {
	case string:
		if v == "" {
			return "\"\""
		}
		return v
	case time.Duration:
		return formatDuration(v)
	default:
		return fmt.Sprintf("%v", def)
	}
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	s := d.String()
	if strings.ContainsAny(s, "hm") && strings.HasSuffix(s, "0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	// Keep minute precision for values like 10m, but drop trailing 0m for exact hours.
	if strings.HasSuffix(s, "0m") && strings.ContainsRune(s, 'h') {
		s = strings.TrimSuffix(s, "0m")
	}
	if s == "" {
		return "0s"
	}
	return s
}

func marshalSortedJSON(v any) ([]byte, error) {
	normalized := sortJSONValue(v)
	return json.MarshalIndent(normalized, "", "  ")
}

func sortJSONValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := make(map[string]any, len(val))
		for _, k := range keys {
			ordered[k] = sortJSONValue(val[k])
		}
		return ordered
	case []any:
		out := make([]any, len(val))
		for i := range val {
			out[i] = sortJSONValue(val[i])
		}
		return out
	default:
		return v
	}
}
