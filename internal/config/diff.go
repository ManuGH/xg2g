// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"reflect"
	"sort"
	"strings"
)

// ChangeSummary describes the result of comparing two AppConfigs.
type ChangeSummary struct {
	ChangedFields   []string // List of field paths that changed
	RestartRequired bool     // True if any changed field is NOT HotReloadable
}

// Diff compares two configurations and returns a summary of changes.
func Diff(old, next AppConfig) (ChangeSummary, error) {
	registry, err := GetRegistry()
	if err != nil {
		return ChangeSummary{}, err
	}

	summary := ChangeSummary{}

	oldVal := reflect.ValueOf(old)
	nextVal := reflect.ValueOf(next)

	summary.compareStruct("", oldVal, nextVal, registry)

	return summary, nil
}

func (s *ChangeSummary) compareStruct(prefix string, oldVal, nextVal reflect.Value, r *Registry) {
	t := oldVal.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		fieldPath := f.Name
		if prefix != "" {
			fieldPath = prefix + "." + f.Name
		}

		ov := oldVal.Field(i)
		nv := nextVal.Field(i)

		// Handle pointers
		if ov.Kind() == reflect.Ptr {
			if ov.IsNil() && nv.IsNil() {
				continue
			}
			if ov.IsNil() != nv.IsNil() {
				s.recordChange(fieldPath, r)
				continue
			}
			ov = ov.Elem()
			nv = nv.Elem()
		}

		if ov.Kind() == reflect.Struct && !isSimpleStruct(ov.Type()) {
			s.compareStruct(fieldPath, ov, nv, r)
			continue
		}

		// Leaf field comparison with normalization
		oNorm := normalizeValue(fieldPath, ov)
		nNorm := normalizeValue(fieldPath, nv)
		if !reflect.DeepEqual(oNorm, nNorm) {
			s.recordChange(fieldPath, r)
		}
	}
}

var (
	// hotReloadAllowlist defines the strictly permitted fields for runtime tuning.
	hotReloadAllowlist = map[string]struct{}{
		"LogLevel": {},
	}
)

func (s *ChangeSummary) recordChange(fieldPath string, r *Registry) {
	s.ChangedFields = append(s.ChangedFields, fieldPath)

	entry, ok := r.ByField[fieldPath]
	_, allowed := hotReloadAllowlist[fieldPath]
	if !ok || !entry.HotReloadable || !allowed {
		s.RestartRequired = true
	}
}

// normalizeValue returns a canonical representation for specific types.
func normalizeValue(fieldPath string, v reflect.Value) any {
	// 1. Handle comma-separated strings
	if v.Kind() == reflect.String && (fieldPath == "Bouquet" || strings.HasSuffix(fieldPath, "Bouquets")) {
		s := v.String()
		if s == "" {
			return ""
		}
		parts := strings.Split(s, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		sort.Strings(parts)
		return strings.Join(parts, ",")
	}

	// 2. Handle string slices: treat nil and empty as same for semantic equality.
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.String {
		if v.Len() == 0 {
			return []string{} // Canonicalize to empty slice
		}
		raw := v.Interface().([]string)
		sorted := make([]string, len(raw))
		copy(sorted, raw)
		sort.Strings(sorted)
		return sorted
	}

	return v.Interface()
}
